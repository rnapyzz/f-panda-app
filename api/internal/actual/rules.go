package actual

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 割当ルール（allocation_rules）。会計科目 × 部門（NULL は全部門）→ 施策。
// 箱の ID で施策が決まらない明細を、受け皿の施策に割り当てる。部門まで一致するルールが優先する。
// ルールを変えても取込済みの明細は変わらない（次の取込・再割当から反映する）。

// departmentPattern は部門コードの形式。CSV で扱えるよう、空白とカンマ・引用符は使えない。
var departmentPattern = regexp.MustCompile(`^[^\s,"]{1,50}$`)

type allocationRule struct {
	ID             int64     `json:"id"`
	GLAccountID    int64     `json:"gl_account_id"`
	GLAccountCode  string    `json:"gl_account_code"`
	GLAccountName  string    `json:"gl_account_name"`
	DepartmentCode *string   `json:"department_code"`
	ActivityID     int64     `json:"activity_id"`
	ActivityCode   string    `json:"activity_code"`
	ActivityName   string    `json:"activity_name"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ruleRequest struct {
	GLAccountID    int64  `json:"gl_account_id"`
	DepartmentCode string `json:"department_code"`
	ActivityID     int64  `json:"activity_id"`
	reasonRequest
}

const ruleSelect = `
	SELECT r.id, r.gl_account_id, g.code, g.name, r.department_code, r.activity_id, a.code, a.name, r.created_at, r.updated_at
	FROM allocation_rules r JOIN gl_accounts g ON g.id = r.gl_account_id JOIN activities a ON a.id = r.activity_id`

func scanRule(row interface{ Scan(...any) error }) (allocationRule, error) {
	var x allocationRule
	var dept sql.NullString
	err := row.Scan(&x.ID, &x.GLAccountID, &x.GLAccountCode, &x.GLAccountName, &dept, &x.ActivityID, &x.ActivityCode, &x.ActivityName, &x.CreatedAt, &x.UpdatedAt)
	if dept.Valid {
		x.DepartmentCode = &dept.String
	}
	return x, err
}

func findRule(ctx context.Context, q dbx.Querier, id int64, lock string) (allocationRule, error) {
	x, err := scanRule(q.QueryRowContext(ctx, ruleSelect+" WHERE r.id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, httpx.NotFound("割当ルールが見つかりません")
	}
	return x, err
}

// listRules は GET /api/allocation-rules。会計科目・部門の順に返す（全部門のルールが先）。
func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), ruleSelect+" ORDER BY g.code, r.department_key")
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []allocationRule
	for rows.Next() {
		x, err := scanRule(rows)
		if err != nil {
			return err
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// validateDepartment は部門コードを検証する。空なら NULL（全部門）。
func validateDepartment(v httpx.Validator, s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !departmentPattern.MatchString(s) {
		v.Add("department_code", "部門コードは空白・カンマ・引用符を含まない50文字以内で入力してください")
	}
	return &s
}

func validateRule(ctx context.Context, tx *sql.Tx, req ruleRequest) (*string, error) {
	v := httpx.Validator{}
	dept := validateDepartment(v, req.DepartmentCode)
	if ok, err := dbx.Exists(ctx, tx, "gl_accounts", req.GLAccountID); err != nil {
		return nil, err
	} else if !ok {
		v.Add("gl_account_id", "会計科目を選んでください")
	}
	if ok, err := dbx.Exists(ctx, tx, "activities", req.ActivityID); err != nil {
		return nil, err
	} else if !ok {
		v.Add("activity_id", "施策を選んでください")
	}
	return dept, v.Err()
}

func duplicateRule(err error) error {
	if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
		return httpx.Validation(map[string]string{"department_code": "この会計科目・部門の割当ルールは既にあります"})
	}
	return err
}

// createRule は POST /api/allocation-rules。
func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) error {
	var req ruleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var created allocationRule
	err := inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		dept, err := validateRule(ctx, tx, req)
		if err != nil {
			return err
		}
		created, err = insertRule(ctx, tx, rec, req.GLAccountID, dept, req.ActivityID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

func insertRule(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, accountID int64, dept *string, activityID int64) (allocationRule, error) {
	res, err := tx.ExecContext(ctx,
		"INSERT INTO allocation_rules (gl_account_id, department_code, activity_id) VALUES (?, ?, ?)",
		accountID, nullDept(dept), activityID)
	if err != nil {
		return allocationRule{}, duplicateRule(err)
	}
	id, _ := res.LastInsertId()
	created, err := findRule(ctx, tx, id, "")
	if err != nil {
		return created, err
	}
	return created, rec.Insert(ctx, "allocation_rules", id, created)
}

func nullDept(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

// updateRule は PUT /api/allocation-rules/{id}。
func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req ruleRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	var updated allocationRule
	err = inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findRule(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		dept, err := validateRule(ctx, tx, req)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE allocation_rules SET gl_account_id = ?, department_code = ?, activity_id = ? WHERE id = ?",
			req.GLAccountID, nullDept(dept), req.ActivityID, id); err != nil {
			return duplicateRule(err)
		}
		if updated, err = findRule(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "allocation_rules", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteRule は DELETE /api/allocation-rules/{id}。取込済みの明細の割当はそのまま残る。
func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request) error {
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
		before, err := findRule(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM allocation_rules WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "allocation_rules", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- CSV ---

var ruleColumns = []string{"account_code", "department_code", "activity_code"}

// exportRules は GET /api/allocation-rules/export。
func (h *Handler) exportRules(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), ruleSelect+" ORDER BY g.code, r.department_key")
	if err != nil {
		return err
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		x, err := scanRule(rows)
		if err != nil {
			return err
		}
		dept := ""
		if x.DepartmentCode != nil {
			dept = *x.DepartmentCode
		}
		out = append(out, []string{x.GLAccountCode, dept, x.ActivityCode})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return csvio.WriteCSV(w, "allocation-rules", ruleColumns, out)
}

// importRules は POST /api/allocation-rules/import。会計科目 × 部門で既存のルールと結びつけ、追加・更新する。
func (h *Handler) importRules(w http.ResponseWriter, r *http.Request) error {
	up, err := csvio.ReadUpload(w, r)
	if err != nil {
		return err
	}
	rows, err := csvio.Parse(up.Data, ruleColumns)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := csvio.Result{DryRun: up.DryRun, Rows: len(rows)}
	err = inTx(r, h.db, up.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		accounts, err := codeIndex(ctx, tx, "SELECT code, id FROM gl_accounts")
		if err != nil {
			return err
		}
		activities, err := codeIndex(ctx, tx, "SELECT code, id FROM activities")
		if err != nil {
			return err
		}
		type item struct {
			account, activity int64
			dept              *string
		}
		errs := &csvio.RowErrors{}
		var items []item
		seen := map[string]bool{}
		for _, row := range rows {
			v := httpx.Validator{}
			var it item
			dept := validateDepartment(v, row.Get("department_code"))
			it.dept = dept
			code := row.Get("account_code")
			if id, ok := accounts[code]; ok {
				it.account = id
			} else {
				v.Add("account_code", "会計科目コード "+code+" は登録されていません")
			}
			ac := row.Get("activity_code")
			if id, ok := activities[ac]; ok {
				it.activity = id
			} else {
				v.Add("activity_code", "施策コード "+ac+" は登録されていません")
			}
			key := code + "\x00" + row.Get("department_code")
			if seen[key] {
				v.Add("department_code", "会計科目 "+code+"・部門 "+row.Get("department_code")+" が CSV 内で重複しています")
			}
			seen[key] = true
			if err := v.Err(); err != nil {
				errs.AddDetails(row.Line, err)
				continue
			}
			items = append(items, it)
		}
		if err := errs.Err(); err != nil {
			return err
		}
		for _, it := range items {
			var id int64
			err := tx.QueryRowContext(ctx,
				"SELECT id FROM allocation_rules WHERE gl_account_id = ? AND department_key = ? FOR UPDATE",
				it.account, deref(it.dept)).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				if _, err := insertRule(ctx, tx, rec, it.account, it.dept, it.activity); err != nil {
					return err
				}
				result.Inserted++
				continue
			}
			if err != nil {
				return err
			}
			before, err := findRule(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if before.ActivityID == it.activity {
				result.Unchanged++
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE allocation_rules SET activity_id = ? WHERE id = ?", it.activity, id); err != nil {
				return err
			}
			after, err := findRule(ctx, tx, id, "")
			if err != nil {
				return err
			}
			if err := rec.Update(ctx, "allocation_rules", id, before, after); err != nil {
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

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
