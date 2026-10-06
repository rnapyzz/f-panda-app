package actual

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 未割当の一覧と割当、再割当（docs/plan.md「2.12」）。

// unallocatedGroup は未割当の明細のまとまり。箱の ID がある行は箱の ID ごと、ない行は会計科目 × 部門ごと。
type unallocatedGroup struct {
	Key            string   `json:"key"`
	BoxCode        *string  `json:"box_code"`
	GLAccountID    *int64   `json:"gl_account_id"` // 箱の ID がないまとまりのみ
	DepartmentCode *string  `json:"department_code"`
	Accounts       []string `json:"accounts"` // 会計科目（「コード 名前」）
	Months         []string `json:"months"`
	Count          int      `json:"count"`
	Revenue        string   `json:"revenue"`
	Expense        string   `json:"expense"`
	Description    string   `json:"description"` // 摘要の例
	revenue        *big.Int
	expense        *big.Int
}

type unallocatedResponse struct {
	Items   []*unallocatedGroup `json:"items"`
	Count   int                 `json:"count"`
	Revenue string              `json:"revenue"`
	Expense string              `json:"expense"`
}

// listUnallocated は GET /api/actuals/unallocated?fiscal_year=。FP&A のみ。
// 年度を省略するとすべての月。金額の大きいまとまりから返す。
func (h *Handler) listUnallocated(w http.ResponseWriter, r *http.Request) error {
	where, args := "", []any{}
	if s := r.URL.Query().Get("fiscal_year"); s != "" {
		fy, err := strconv.Atoi(s)
		if err != nil {
			return httpx.BadRequest("fiscal_year は数値で指定してください")
		}
		months := calc.FiscalMonths(fy)
		where = " AND e.target_month BETWEEN ? AND ?"
		args = append(args, months[0]+"-01", months[len(months)-1]+"-01")
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT e.box_code, e.gl_account_id, g.code, g.name, e.department_code, DATE_FORMAT(e.target_month, '%Y-%m'), s.category,
		       COUNT(*), CAST(SUM(e.amount) AS CHAR), MIN(COALESCE(e.description, ''))
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id JOIN subjects s ON s.id = e.subject_id
		WHERE e.activity_id IS NULL`+where+`
		GROUP BY e.box_code, e.gl_account_id, g.code, g.name, e.department_code, e.target_month, s.category
		ORDER BY e.target_month`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	groups := map[string]*unallocatedGroup{}
	resp := unallocatedResponse{Items: []*unallocatedGroup{}}
	revenue, expense := new(big.Int), new(big.Int)
	for rows.Next() {
		var box, dept sql.NullString
		var accountID int64
		var code, name, month, category, amount, desc string
		var n int
		if err := rows.Scan(&box, &accountID, &code, &name, &dept, &month, &category, &n, &amount, &desc); err != nil {
			return err
		}
		key := "box:" + box.String
		if !box.Valid {
			key = fmt.Sprintf("account:%d:%s", accountID, dept.String)
		}
		g := groups[key]
		if g == nil {
			g = &unallocatedGroup{Key: key, Accounts: []string{}, Months: []string{}, revenue: new(big.Int), expense: new(big.Int)}
			if box.Valid {
				g.BoxCode = &box.String
			} else {
				id := accountID
				g.GLAccountID = &id
				if dept.Valid {
					g.DepartmentCode = &dept.String
				}
			}
			groups[key] = g
			resp.Items = append(resp.Items, g)
		}
		if label := code + " " + name; !contains(g.Accounts, label) {
			g.Accounts = append(g.Accounts, label)
		}
		if !contains(g.Months, month) {
			g.Months = append(g.Months, month)
		}
		if g.Description == "" {
			g.Description = desc
		}
		g.Count += n
		resp.Count += n
		v, _ := new(big.Int).SetString(amount, 10)
		if category == "revenue" {
			g.revenue.Add(g.revenue, v)
			revenue.Add(revenue, v)
		} else {
			g.expense.Add(g.expense, v)
			expense.Add(expense, v)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	size := func(g *unallocatedGroup) *big.Int {
		return new(big.Int).Add(new(big.Int).Abs(g.revenue), new(big.Int).Abs(g.expense))
	}
	sort.SliceStable(resp.Items, func(i, j int) bool { return size(resp.Items[i]).Cmp(size(resp.Items[j])) > 0 })
	for _, g := range resp.Items {
		g.Revenue, g.Expense = g.revenue.String(), g.expense.String()
	}
	resp.Revenue, resp.Expense = revenue.String(), expense.String()
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// externalCodePattern は外部コードの形式（施策 API と同じ）。
var externalCodePattern = regexp.MustCompile(`^[^\s,"]{1,100}$`)

type assignRequest struct {
	BoxCode        *string `json:"box_code"`
	GLAccountID    *int64  `json:"gl_account_id"`
	DepartmentCode *string `json:"department_code"`
	// AllDepartments は、会計科目 × 部門のまとまりを、部門を問わない割当ルール（全部門）にする
	AllDepartments bool   `json:"all_departments"`
	ActivityID     int64  `json:"activity_id"`
	Reason         string `json:"reason"`
}

type assignResult struct {
	Assigned int `json:"assigned"` // 割り当てた明細の行数
	// SkippedClosed は、締めた年度の月なので割り当てなかった未割当の行数（ルールは登録する）
	SkippedClosed int      `json:"skipped_closed"`
	Months        []string `json:"months"`
	// ExternalCode は登録した外部コード、Rule は追加した割当ルール（どちらか一方）
	ExternalCode *string         `json:"external_code,omitempty"`
	Rule         *allocationRule `json:"rule,omitempty"`
	factCounts
}

// externalCodeRecord は監査ログに残す外部コード（施策 API と同じ形）。
type externalCodeRecord struct {
	ID         int64     `json:"id"`
	ActivityID int64     `json:"activity_id"`
	Code       string    `json:"code"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// assignUnallocated は POST /api/actuals/unallocated/assign。FP&A のみ。
//
// 未割当のまとまりを施策に割り当て、翌月以降も同じ施策に入るようにルールとして残す。
//   - box_code: 箱の ID を施策の外部コードとして登録し、その箱の ID の未割当の明細（すべての月）を割り当てる
//   - gl_account_id（と department_code）: 割当ルールを追加し、その会計科目 × 部門の未割当の明細を割り当てる。
//     all_departments なら部門を問わないルールにする
func (h *Handler) assignUnallocated(w http.ResponseWriter, r *http.Request) error {
	var req assignRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		v.Add("reason", "変更理由を入力してください")
	}
	if (req.BoxCode == nil) == (req.GLAccountID == nil) {
		v.Add("box_code", "箱の ID か会計科目のどちらかを指定してください")
	}
	if req.ActivityID == 0 {
		v.Add("activity_id", "割り当てる施策を選んでください")
	}
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var result assignResult
	err := inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		activity, err := findActivity(ctx, tx, req.ActivityID)
		if err != nil {
			return err
		}
		where, args := "", []any{}
		if req.BoxCode != nil {
			code := strings.TrimSpace(*req.BoxCode)
			if err := registerExternalCode(ctx, tx, rec, code, activity); err != nil {
				return err
			}
			result.ExternalCode = &code
			where, args = "box_code = ?", []any{code}
		} else {
			if _, err := findAccount(ctx, tx, *req.GLAccountID, ""); err != nil {
				return httpx.Validation(map[string]string{"gl_account_id": "会計科目が見つかりません"})
			}
			var dept *string
			if !req.AllDepartments && req.DepartmentCode != nil && *req.DepartmentCode != "" {
				dept = req.DepartmentCode
			}
			rule, err := insertRule(ctx, tx, rec, *req.GLAccountID, dept, req.ActivityID)
			if err != nil {
				if isDuplicateRule(err) {
					return httpx.Conflict("この会計科目・部門の割当ルールは既にあります。再割当で今のルールを当て直してください")
				}
				return err
			}
			result.Rule = &rule
			// 割当の順番（箱の ID → 割当ルール）に合わせ、箱の ID で決まらなかった行はルールで割り当てる。
			// 部門が空のルールは全部門に当たるので、部門を問わずに割り当てる
			where, args = "gl_account_id = ?", []any{*req.GLAccountID}
			if dept != nil {
				where += " AND department_code = ?"
				args = append(args, *dept)
			}
		}

		// 締めた年度の行は割り当てない（実績の数字を変えないため）
		if err := tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM actual_entries WHERE activity_id IS NULL AND "+where+" AND NOT "+notClosedCondition, args...).Scan(&result.SkippedClosed); err != nil {
			return err
		}
		where += " AND " + notClosedCondition
		months, err := stringColumn(ctx, tx, `
			SELECT DISTINCT DATE_FORMAT(target_month, '%Y-%m') FROM actual_entries
			WHERE activity_id IS NULL AND `+where+` ORDER BY 1 FOR UPDATE`, args...)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"UPDATE actual_entries SET activity_id = ?, allocated_by = ? WHERE activity_id IS NULL AND "+where,
			append([]any{req.ActivityID, byManual}, args...)...)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		result.Assigned = int(n)
		result.Months = months
		return syncFacts(ctx, tx, rec, months, &result.factCounts)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func isDuplicateRule(err error) bool {
	var apiErr *httpx.Error
	return errors.As(err, &apiErr) && apiErr.Details["department_code"] != ""
}

// registerExternalCode は箱の ID を施策の外部コードとして登録する（施策 API の外部コードの登録と同じ検証）。
func registerExternalCode(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, code string, activity activityRef) error {
	if !externalCodePattern.MatchString(code) {
		return httpx.Validation(map[string]string{"box_code": "外部コードに空白・カンマ・引用符は使えません（100文字以内）"})
	}
	var name string
	err := tx.QueryRowContext(ctx, "SELECT name FROM activities WHERE code = ?", code).Scan(&name)
	if err == nil {
		return httpx.Conflict(fmt.Sprintf("箱の ID %q は施策「%s」の施策コードです。再割当で今のルールを当て直してください", code, name))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT a.name FROM activity_external_codes e JOIN activities a ON a.id = e.activity_id WHERE e.code = ?`, code).Scan(&name)
	if err == nil {
		return httpx.Conflict(fmt.Sprintf("箱の ID %q は施策「%s」の外部コードとして登録済みです。再割当で今のルールを当て直してください", code, name))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	res, err := tx.ExecContext(ctx,
		"INSERT INTO activity_external_codes (activity_id, code, note) VALUES (?, ?, ?)", activity.ID, code, "未割当の一覧から登録")
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	var created externalCodeRecord
	err = tx.QueryRowContext(ctx,
		"SELECT id, activity_id, code, COALESCE(note, ''), created_at, updated_at FROM activity_external_codes WHERE id = ?", id,
	).Scan(&created.ID, &created.ActivityID, &created.Code, &created.Note, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		return err
	}
	return rec.Insert(ctx, "activity_external_codes", id, created)
}

func stringColumn(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- 再割当 ---

type reallocateRequest struct {
	Months []string `json:"months"`
	Reason string   `json:"reason"`
	DryRun bool     `json:"dry_run"`
}

// activityChange は再割当による施策ごとの増減（収益・費用）。施策が nil は未割当。
type activityChange struct {
	Activity *activityRef `json:"activity"`
	Revenue  string       `json:"revenue"`
	Expense  string       `json:"expense"`
	revenue  *big.Int
	expense  *big.Int
}

type reallocateResult struct {
	DryRun  bool              `json:"dry_run"`
	Months  []string          `json:"months"`
	Entries int               `json:"entries"` // 対象の明細の行数
	Changed int               `json:"changed"` // 割当が変わった行数
	Removed int               `json:"removed"` // 対象外になった会計科目の行数
	Changes []*activityChange `json:"changes"`
	factCounts
}

// reallocate は POST /api/actuals/reallocate。FP&A のみ。
// 指定した月の明細に、今の外部コード・割当ルール・会計科目の対応を当て直す。対象外になった会計科目の行は除く。
// dry_run なら保存せず、施策ごとの増減だけを返す。
func (h *Handler) reallocate(w http.ResponseWriter, r *http.Request) error {
	var req reallocateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" && !req.DryRun {
		v.Add("reason", "変更理由を入力してください")
	}
	monthSet := map[string]bool{}
	for _, m := range req.Months {
		if !isYearMonth(m) {
			v.Add("months", "月は YYYY-MM 形式で指定してください")
			break
		}
		monthSet[m] = true
	}
	if len(monthSet) == 0 {
		v.Add("months", "再割当する月を選んでください")
	}
	if err := v.Err(); err != nil {
		return err
	}
	months := make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)

	ctx := r.Context()
	result := reallocateResult{DryRun: req.DryRun, Months: months, Changes: []*activityChange{}}
	err := inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		closed, err := closedYears(ctx, tx)
		if err != nil {
			return err
		}
		if cm := closedMonths(months, closed); len(cm) > 0 {
			return httpx.Validation(map[string]string{"months": "締めた年度の月は再割当できません: " + strings.Join(cm, "、")})
		}
		have, err := entryMonths(ctx, tx, months)
		if err != nil {
			return err
		}
		var missing []string
		for _, m := range months {
			if !have[m] {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			return httpx.Validation(map[string]string{"months": "明細のない月は再割当できません: " + strings.Join(missing, "、")})
		}
		accounts, err := loadAccounts(ctx, tx)
		if err != nil {
			return err
		}
		al, err := loadAllocator(ctx, tx)
		if err != nil {
			return err
		}
		categories, err := subjectCategories(ctx, tx)
		if err != nil {
			return err
		}

		type row struct {
			id, accountID, subjectID int64
			dept, box                string
			amount                   *big.Int
			activityID               *int64
			by                       string
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id, gl_account_id, subject_id, COALESCE(department_code, ''), COALESCE(box_code, ''), CAST(amount AS CHAR), activity_id, allocated_by
			FROM actual_entries WHERE target_month IN (`+placeholders(len(months))+`) FOR UPDATE`, monthArgs(months)...)
		if err != nil {
			return err
		}
		var list []row
		for rows.Next() {
			var x row
			var amount string
			var activity sql.NullInt64
			if err := rows.Scan(&x.id, &x.accountID, &x.subjectID, &x.dept, &x.box, &amount, &activity, &x.by); err != nil {
				rows.Close()
				return err
			}
			x.amount, _ = new(big.Int).SetString(amount, 10)
			x.activityID = dbx.PtrInt64(activity)
			list = append(list, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		result.Entries = len(list)

		changes := map[int64]*activityChange{}
		move := func(activityID *int64, subjectID int64, amount *big.Int, sign int) {
			key := int64(0)
			if activityID != nil {
				key = *activityID
			}
			c := changes[key]
			if c == nil {
				c = &activityChange{revenue: new(big.Int), expense: new(big.Int)}
				if activityID != nil {
					c.Activity = &activityRef{ID: *activityID}
				}
				changes[key] = c
			}
			delta := new(big.Int).Mul(amount, big.NewInt(int64(sign)))
			if categories[subjectID] == "revenue" {
				c.revenue.Add(c.revenue, delta)
			} else {
				c.expense.Add(c.expense, delta)
			}
		}
		for _, x := range list {
			acc := accounts[x.accountID]
			if acc.IsExcluded {
				if _, err := tx.ExecContext(ctx, "DELETE FROM actual_entries WHERE id = ?", x.id); err != nil {
					return err
				}
				move(x.activityID, x.subjectID, x.amount, -1)
				result.Removed++
				continue
			}
			activityID, by := al.allocate(x.accountID, x.dept, x.box)
			subjectID := *acc.SubjectID
			if equalPtr(activityID, x.activityID) && by == x.by && subjectID == x.subjectID {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE actual_entries SET activity_id = ?, allocated_by = ?, subject_id = ? WHERE id = ?",
				activityArg(activityID), by, subjectID, x.id); err != nil {
				return err
			}
			result.Changed++
			if !equalPtr(activityID, x.activityID) || subjectID != x.subjectID {
				move(x.activityID, x.subjectID, x.amount, -1)
				move(activityID, subjectID, x.amount, 1)
			}
		}
		if err := syncFacts(ctx, tx, rec, months, &result.factCounts); err != nil {
			return err
		}

		for _, c := range changes {
			if c.revenue.Sign() == 0 && c.expense.Sign() == 0 {
				continue
			}
			if c.Activity != nil {
				a, err := findActivity(ctx, tx, c.Activity.ID)
				if err != nil {
					return err
				}
				c.Activity = &a
			}
			c.Revenue, c.Expense = c.revenue.String(), c.expense.String()
			result.Changes = append(result.Changes, c)
		}
		sort.Slice(result.Changes, func(i, j int) bool {
			a, b := result.Changes[i].Activity, result.Changes[j].Activity
			if a == nil || b == nil {
				return b == nil && a != nil
			}
			return a.Code < b.Code
		})
		if req.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

// subjectCategories は科目 ID → 区分（revenue / expense）を返す。
func subjectCategories(ctx context.Context, tx *sql.Tx) (map[int64]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, category FROM subjects")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var c string
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
