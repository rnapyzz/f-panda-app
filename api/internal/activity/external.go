package activity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/codes"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// externalCodePattern は外部コードの形式。CSV で扱えるよう、空白とカンマ・引用符は使えない。
var externalCodePattern = regexp.MustCompile(`^[^\s,"]{1,100}$`)

// autoCodePrefix は自動採番する施策コードの接頭辞（ACT-0001 の形式）。
const autoCodePrefix = "ACT-"

// externalCode は施策の外部コード（会計・基幹システムの案件番号など）。
type externalCode struct {
	ID         int64  `json:"id"`
	ActivityID int64  `json:"activity_id"`
	Code       string `json:"code"`
	Note       string `json:"note"`
	timestamps
}

const externalCodeSelect = "SELECT id, activity_id, code, COALESCE(note, ''), created_at, updated_at FROM activity_external_codes"

func scanExternalCode(row interface{ Scan(...any) error }) (externalCode, error) {
	var e externalCode
	err := row.Scan(&e.ID, &e.ActivityID, &e.Code, &e.Note, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func listExternalCodes(ctx context.Context, q querier, activityID int64) ([]externalCode, error) {
	rows, err := q.QueryContext(ctx, externalCodeSelect+" WHERE activity_id = ? ORDER BY code", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []externalCode{}
	for rows.Next() {
		e, err := scanExternalCode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// nextActivityCode は自動採番の次の施策コード（ACT-0001, ACT-0002, ...）を返す。
// 外部コードと同じ値は避ける（CSV で施策を特定できなくなるため）。
func nextActivityCode(ctx context.Context, tx *sql.Tx) (string, error) {
	return codes.Next(ctx, tx, "activities", autoCodePrefix, func(code string) (bool, error) {
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activity_external_codes WHERE code = ?", code)
		return n > 0, err
	})
}

// checkCodeNotExternal は施策コードが既存の外部コードと重ならないことを確認する。
func checkCodeNotExternal(ctx context.Context, tx *sql.Tx, code string) error {
	n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activity_external_codes WHERE code = ?", code)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.Validation(map[string]string{"code": "この施策コードは外部コードとして使われています"})
	}
	return nil
}

type externalCodeRequest struct {
	Code   string `json:"code"`
	Note   string `json:"note"`
	Reason string `json:"reason"`
}

// createExternalCode は POST /api/activities/{id}/external-codes。
// 外部コードは、他の施策の外部コードや施策コードと重ならないこと。
func (h *Handler) createExternalCode(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req externalCodeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	code := v.Text("code", "外部コード", req.Code, 100)
	if code != "" && !externalCodePattern.MatchString(code) {
		v.Add("code", "外部コードに空白・カンマ・引用符は使えません")
	}
	note := v.OptionalText("note", "メモ", req.Note, 200)
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var created externalCode
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		// 施策コードとの重複
		var owner struct {
			id   int64
			name string
		}
		err := tx.QueryRowContext(ctx, "SELECT id, name FROM activities WHERE code = ?", code).Scan(&owner.id, &owner.name)
		if err == nil {
			return httpx.Validation(map[string]string{"code": fmt.Sprintf("施策「%s」の施策コードと同じです", owner.name)})
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// 他の施策の外部コードとの重複（どの施策かを示す）
		err = tx.QueryRowContext(ctx, `
			SELECT a.id, a.name FROM activity_external_codes e JOIN activities a ON a.id = e.activity_id WHERE e.code = ?`, code,
		).Scan(&owner.id, &owner.name)
		if err == nil {
			if owner.id == activityID {
				return httpx.Validation(map[string]string{"code": "この施策に登録済みです"})
			}
			return httpx.Validation(map[string]string{"code": fmt.Sprintf("施策「%s」に登録されています（外部コードは1つの施策にだけ紐づけられます）", owner.name)})
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		res, err := tx.ExecContext(ctx,
			"INSERT INTO activity_external_codes (activity_id, code, note) VALUES (?, ?, ?)",
			activityID, code, dbx.NullString(note),
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "この外部コードは既に登録されています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = scanExternalCode(tx.QueryRowContext(ctx, externalCodeSelect+" WHERE id = ?", id)); err != nil {
			return err
		}
		return rec.Insert(ctx, "activity_external_codes", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// deleteExternalCode は DELETE /api/activities/{id}/external-codes/{eid}。
// 取込済みの実績は施策に紐づいているため、外部コードを外しても実績は残る。
func (h *Handler) deleteExternalCode(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "eid")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := scanExternalCode(tx.QueryRowContext(ctx, externalCodeSelect+" WHERE id = ? AND activity_id = ? FOR UPDATE", id, activityID))
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("外部コードが見つかりません")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity_external_codes WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "activity_external_codes", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
