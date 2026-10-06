package actual

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/codes"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 会計科目（gl_accounts）。会計システムの勘定科目と、アプリの科目への対応。
// 対象外（P/L に関係ない会計科目）の行は取り込まない。hide_details の会計科目は、明細を FP&A 以外に見せない。

type glAccount struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	SubjectID   *int64    `json:"subject_id"`
	IsExcluded  bool      `json:"is_excluded"`
	HideDetails bool      `json:"hide_details"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type glAccountRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	SubjectID   *int64 `json:"subject_id"`
	IsExcluded  bool   `json:"is_excluded"`
	HideDetails bool   `json:"hide_details"`
	reasonRequest
}

const accountSelect = "SELECT id, code, name, subject_id, is_excluded, hide_details, created_at, updated_at FROM gl_accounts"

func scanAccount(row interface{ Scan(...any) error }) (glAccount, error) {
	var a glAccount
	var subject sql.NullInt64
	err := row.Scan(&a.ID, &a.Code, &a.Name, &subject, &a.IsExcluded, &a.HideDetails, &a.CreatedAt, &a.UpdatedAt)
	a.SubjectID = dbx.PtrInt64(subject)
	return a, err
}

func findAccount(ctx context.Context, q dbx.Querier, id int64, lock string) (glAccount, error) {
	a, err := scanAccount(q.QueryRowContext(ctx, accountSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, httpx.NotFound("会計科目が見つかりません")
	}
	return a, err
}

// loadAccounts は全会計科目を ID → 会計科目で返す。
func loadAccounts(ctx context.Context, tx *sql.Tx) (map[int64]glAccount, error) {
	rows, err := tx.QueryContext(ctx, accountSelect)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]glAccount{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

// listAccounts は GET /api/gl-accounts。コードの順に返す。
func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), accountSelect+" ORDER BY code")
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []glAccount
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return err
		}
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// validateAccount は会計科目の入力を検証する。対象外なら科目は持たず、対象外でなければ科目が必須。
func validateAccount(ctx context.Context, tx *sql.Tx, req *glAccountRequest) error {
	v := httpx.Validator{}
	req.Code = strings.TrimSpace(req.Code)
	if !codes.Pattern.MatchString(req.Code) {
		v.Add("code", codes.PatternMessage)
	}
	req.Name = v.Text("name", "名前", req.Name, 100)
	if req.IsExcluded {
		req.SubjectID = nil
	} else if req.SubjectID == nil {
		v.Add("subject_id", "対応する科目を選んでください（P/L に関係ない会計科目は「対象外」にします）")
	} else if ok, err := dbx.Exists(ctx, tx, "subjects", *req.SubjectID); err != nil {
		return err
	} else if !ok {
		v.Add("subject_id", "科目が見つかりません")
	}
	return v.Err()
}

func duplicateAccountCode(err error) error {
	if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
		return httpx.Validation(map[string]string{"code": "このコードは既に使われています"})
	}
	return err
}

// createAccount は POST /api/gl-accounts。
func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) error {
	var req glAccountRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var created glAccount
	err := inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := validateAccount(ctx, tx, &req); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO gl_accounts (code, name, subject_id, is_excluded, hide_details) VALUES (?, ?, ?, ?, ?)",
			req.Code, req.Name, dbx.NullInt64(req.SubjectID), req.IsExcluded, req.HideDetails)
		if err != nil {
			return duplicateAccountCode(err)
		}
		id, _ := res.LastInsertId()
		if created, err = findAccount(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "gl_accounts", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateAccount は PUT /api/gl-accounts/{id}。
// 科目の対応や対象外を変えても、取込済みの明細は変わらない（次の取込・再割当から反映する）。
func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req glAccountRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var updated glAccount
	err = inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findAccount(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if err := validateAccount(ctx, tx, &req); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE gl_accounts SET code = ?, name = ?, subject_id = ?, is_excluded = ?, hide_details = ? WHERE id = ?",
			req.Code, req.Name, dbx.NullInt64(req.SubjectID), req.IsExcluded, req.HideDetails, id); err != nil {
			return duplicateAccountCode(err)
		}
		if updated, err = findAccount(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "gl_accounts", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteAccount は DELETE /api/gl-accounts/{id}。明細・割当ルールから参照されている会計科目は削除できない。
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	err = inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findAccount(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM gl_accounts WHERE id = ?", id); err != nil {
			if dbx.ErrNo(err) == dbx.ErrRowIsReferenced {
				return httpx.Conflict("会計科目は明細または割当ルールから参照されているため削除できません")
			}
			return err
		}
		return rec.Delete(ctx, "gl_accounts", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- CSV ---

var accountColumns = []string{"code", "name", "subject_code", "hide_details"}

// exportAccounts は GET /api/gl-accounts/export。対象外の会計科目は subject_code を空にする。
func (h *Handler) exportAccounts(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT g.code, g.name, COALESCE(s.code, ''), g.hide_details
		FROM gl_accounts g LEFT JOIN subjects s ON s.id = g.subject_id ORDER BY g.code`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		var code, name, subject string
		var hide bool
		if err := rows.Scan(&code, &name, &subject, &hide); err != nil {
			return err
		}
		out = append(out, []string{code, name, subject, strconv.FormatBool(hide)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "gl-accounts", accountColumns, out)
}

// importAccounts は POST /api/gl-accounts/import。コードで既存の会計科目と結びつけ、追加・更新する。
// subject_code が空の行は対象外にする。
func (h *Handler) importAccounts(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, accountColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}
	err = inTx(r, h.db, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		subjects, err := codeIndex(ctx, tx, "SELECT code, id FROM subjects")
		if err != nil {
			return err
		}
		errs := &csvio.RowErrors{}
		var items []glAccountRequest
		seen := map[string]bool{}
		for _, row := range rows {
			req := glAccountRequest{Code: row.Get("code"), Name: row.Get("name")}
			v := httpx.Validator{}
			if !codes.Pattern.MatchString(req.Code) {
				v.Add("code", codes.PatternMessage)
			}
			req.Name = v.Text("name", "名前", req.Name, 100)
			if sc := row.Get("subject_code"); sc == "" {
				req.IsExcluded = true
			} else if id, ok := subjects[sc]; ok {
				req.SubjectID = &id
			} else {
				v.Add("subject_code", "科目コード "+sc+" は登録されていません")
			}
			hide, ok := parseBool(row.Get("hide_details"))
			if !ok {
				v.Add("hide_details", "hide_details は true / false で入力してください")
			}
			req.HideDetails = hide
			if seen[req.Code] {
				v.Add("code", "コード "+req.Code+" が CSV 内で重複しています")
			}
			seen[req.Code] = true
			if err := v.Err(); err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			items = append(items, req)
		}
		if err := errs.Err(); err != nil {
			return err
		}
		for _, it := range items {
			var id int64
			err := tx.QueryRowContext(ctx, "SELECT id FROM gl_accounts WHERE code = ? FOR UPDATE", it.Code).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO gl_accounts (code, name, subject_id, is_excluded, hide_details) VALUES (?, ?, ?, ?, ?)",
					it.Code, it.Name, dbx.NullInt64(it.SubjectID), it.IsExcluded, it.HideDetails)
				if err != nil {
					return err
				}
				id, _ = res.LastInsertId()
				created, err := findAccount(ctx, tx, id, "")
				if err != nil {
					return err
				}
				if err := rec.Insert(ctx, "gl_accounts", id, created); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if err != nil {
				return err
			}
			before, err := findAccount(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if before.Name == it.Name && equalPtr(before.SubjectID, it.SubjectID) && before.IsExcluded == it.IsExcluded && before.HideDetails == it.HideDetails {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx,
				"UPDATE gl_accounts SET name = ?, subject_id = ?, is_excluded = ?, hide_details = ? WHERE id = ?",
				it.Name, dbx.NullInt64(it.SubjectID), it.IsExcluded, it.HideDetails, id); err != nil {
				return err
			}
			after, err := findAccount(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "gl_accounts", id, before, after); err != nil {
				return err
			}
			result.Updated++
		}
		if up.DryRun {
			return csvio.ErrDryRun
		}
		return nil
	})
	return csvio.Finish(w, result, err)
}

// codeIndex は、コードと ID を返すクエリから code → id の対応表を作る。
func codeIndex(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		out[code] = id
	}
	return out, rows.Err()
}

// parseBool は true / false（1 / 0 も可）を読む。空欄は false とする。
func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true, true
	case "", "false", "0", "no":
		return false, true
	}
	return false, false
}

func equalPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
