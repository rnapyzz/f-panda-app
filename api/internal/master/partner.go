package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/codes"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/visibility"
)

// 取引先（docs/plan.md「2.29」）。施策の相手（顧客・発注先）。内訳ごとに 0〜1件付ける。
// 内訳は取引先の ID を指すので、名前を直しても紐づきは変わらない。組織変更では新しい取引先を作り、古いほうを取引停止にする。

// partnerCodePrefix は自動採番する取引先コードの接頭辞（TP-0001 の形式）。
const partnerCodePrefix = "TP-"

const maxPartnerNoteLen = 500

// partner は取引先。
type partner struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	CompanyName string `json:"company_name"`
	// AccountingCode は会計システムの得意先・仕入先のコード（nil は未登録）
	AccountingCode *string `json:"accounting_code"`
	// Direction は区分（sales: 受注＝顧客、purchase: 発注＝発注先）
	Direction string `json:"direction"`
	// IsActive は取引中か（false は取引停止。内訳の選択肢に出さない）
	IsActive bool   `json:"is_active"`
	Note     string `json:"note"`
	// LineCount は付いている内訳の数
	LineCount int `json:"line_count"`
	timestamps
}

type partnerRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	CompanyName    string `json:"company_name"`
	AccountingCode string `json:"accounting_code"`
	Direction      string `json:"direction"`
	// IsActive は省略すると、作成では取引中、更新では今のまま
	IsActive *bool  `json:"is_active"`
	Note     string `json:"note"`
	reasonRequest
}

const partnerSelect = `SELECT p.id, p.code, p.name, p.company_name, p.accounting_code, p.direction, p.is_active, COALESCE(p.note, ''),
	(SELECT COUNT(*) FROM activity_lines l WHERE l.partner_id = p.id), p.created_at, p.updated_at FROM partners p`

func scanPartner(row interface{ Scan(...any) error }) (partner, error) {
	var p partner
	var accounting sql.NullString
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.CompanyName, &accounting, &p.Direction, &p.IsActive, &p.Note, &p.LineCount, &p.CreatedAt, &p.UpdatedAt)
	if accounting.Valid {
		p.AccountingCode = &accounting.String
	}
	return p, err
}

func findPartner(ctx context.Context, q dbx.Querier, id int64, lock string) (partner, error) {
	p, err := scanPartner(q.QueryRowContext(ctx, partnerSelect+" WHERE p.id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return partner{}, notFound("取引先")
	}
	return p, err
}

// partnerInput は検証済みの入力。
type partnerInput struct {
	code, name, company, accounting, direction, note string
}

func validatePartner(req partnerRequest, allowEmptyCode bool) (partnerInput, error) {
	v := httpx.Validator{}
	in := partnerInput{
		code:       validateCode(v, req.Code, allowEmptyCode),
		name:       v.Text("name", "取引先名", req.Name, maxNameLen),
		company:    v.Text("company_name", "会社名", req.CompanyName, maxNameLen),
		accounting: v.OptionalText("accounting_code", "会計システムコード", req.AccountingCode, 50),
		direction:  req.Direction,
		note:       v.OptionalText("note", "メモ", req.Note, maxPartnerNoteLen),
	}
	if in.direction != "sales" && in.direction != "purchase" {
		v.Add("direction", "区分は sales（受注）か purchase（発注）を指定してください")
	}
	return in, v.Err()
}

// partnerDuplicate は一意制約の違反を、どの項目かが分かる入力エラーにする。
func partnerDuplicate(err error) error {
	if dbx.ErrNo(err) != dbx.ErrDuplicateEntry {
		return err
	}
	if strings.Contains(err.Error(), "uq_partners_accounting_code") {
		return httpx.Validation(map[string]string{"accounting_code": "この会計システムコードは既にほかの取引先で使われています"})
	}
	return httpx.Validation(map[string]string{"code": "この取引先コードは既に使われています"})
}

// listPartners は GET /api/partners。会社名・取引先名の順に返す。
func (h *Handler) listPartners(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), partnerSelect+" ORDER BY p.company_name, p.name, p.code")
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []partner{}
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// getPartner は GET /api/partners/{id}。
func (h *Handler) getPartner(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := findPartner(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// createPartner は POST /api/partners。取引先コードが空なら TP-0001 形式で採番する。
func (h *Handler) createPartner(w http.ResponseWriter, r *http.Request) error {
	var req partnerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	in, err := validatePartner(req, true)
	if err != nil {
		return err
	}
	active := req.IsActive == nil || *req.IsActive

	ctx := r.Context()
	var created partner
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if in.code == "" {
			if in.code, err = codes.Next(ctx, tx, "partners", partnerCodePrefix, nil); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO partners (code, name, company_name, accounting_code, direction, is_active, note) VALUES (?, ?, ?, ?, ?, ?, ?)",
			in.code, in.name, in.company, dbx.NullString(in.accounting), in.direction, active, dbx.NullString(in.note))
		if err := partnerDuplicate(err); err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findPartner(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "partners", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updatePartner は PUT /api/partners/{id}。取引先コードが空なら変えない。
// 内訳に付いている取引先の区分は、その内訳の科目の区分と合わなくなる変更はできない。
func (h *Handler) updatePartner(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req partnerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	in, err := validatePartner(req, true)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated partner
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findPartner(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if in.code == "" {
			in.code = before.Code
		}
		active := before.IsActive
		if req.IsActive != nil {
			active = *req.IsActive
		}
		if in.direction != before.Direction {
			n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activity_lines WHERE partner_id = ?", id)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.Validation(map[string]string{"direction": "内訳に付いている取引先の区分は変えられません。新しい取引先を作ってください"})
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE partners SET code = ?, name = ?, company_name = ?, accounting_code = ?, direction = ?, is_active = ?, note = ? WHERE id = ?",
			in.code, in.name, in.company, dbx.NullString(in.accounting), in.direction, active, dbx.NullString(in.note), id)
		if err := partnerDuplicate(err); err != nil {
			return err
		}
		if updated, err = findPartner(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "partners", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deletePartner は DELETE /api/partners/{id}。内訳に付いている取引先は削除できない（取引停止にする）。
func (h *Handler) deletePartner(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findPartner(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if before.LineCount > 0 {
			return httpx.Conflict("内訳に付いているため削除できません。取引停止にしてください")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM partners WHERE id = ?", id); err != nil {
			return deleteError(err, "取引先")
		}
		return rec.Delete(ctx, "partners", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// partnerLine は取引先が付いた内訳。
type partnerLine struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	SubjectID    int64  `json:"subject_id"`
	SubjectName  string `json:"subject_name"`
	ActivityID   int64  `json:"activity_id"`
	ActivityCode string `json:"activity_code"`
	ActivityName string `json:"activity_name"`
	Status       string `json:"status"`
}

// partnerLines は GET /api/partners/{id}/lines。取引先が付いた内訳と施策（施策コードの順）。
// 閲覧制限のある科目の内訳は、見られない人には返さない（docs/plan.md「2.17」）。
func (h *Handler) partnerLines(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("ログインしてください")
	}
	ctx := r.Context()
	if _, err := findPartner(ctx, h.db, id, ""); err != nil {
		return err
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT l.id, l.name, s.id, s.name, a.id, a.code, a.name, a.status
		FROM activity_lines l
		JOIN subjects s ON s.id = l.subject_id
		JOIN activities a ON a.id = l.activity_id
		WHERE l.partner_id = ?`+visibility.SubjectFilter(u, "l.subject_id")+`
		ORDER BY a.code, s.sort_order, l.sort_order, l.id`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []partnerLine{}
	for rows.Next() {
		var l partnerLine
		if err := rows.Scan(&l.ID, &l.Name, &l.SubjectID, &l.SubjectName, &l.ActivityID, &l.ActivityCode, &l.ActivityName, &l.Status); err != nil {
			return err
		}
		items = append(items, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}
