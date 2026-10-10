package actual

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/csvio"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/visibility"
)

// 施策の実績の明細（docs/plan.md「2.12」の実績の明細）。
//
// 既定はすべての月で、新しい月から順に返す。年度・月・科目で絞り込める。画面は 100 件ずつ読み込み、
// 合計（明細の合計・実績データの合計）は絞り込んだ全件で返す。エクスポートは絞り込みどおりの全件を CSV で返す。
// hide_details の会計科目は、FP&A 以外には明細を出さず、会計科目 × 科目（CSV は月も）の合計だけを返す。
// 閲覧制限のある科目（docs/plan.md「2.17」）は、明細も合計も出さない。

const (
	entriesPageSize = 100
	entriesMaxPage  = 500
)

type entryItem struct {
	ID             int64   `json:"id"`
	TargetMonth    string  `json:"target_month"`
	GLAccountCode  string  `json:"gl_account_code"`
	GLAccountName  string  `json:"gl_account_name"`
	SubjectID      int64   `json:"subject_id"`
	DepartmentCode *string `json:"department_code"`
	BoxCode        *string `json:"box_code"`
	Description    string  `json:"description"`
	Amount         string  `json:"amount"`
	AllocatedBy    string  `json:"allocated_by"`
}

// hiddenTotal は明細を見せない会計科目の、会計科目 × 科目の合計。
type hiddenTotal struct {
	GLAccountCode string `json:"gl_account_code"`
	GLAccountName string `json:"gl_account_name"`
	SubjectID     int64  `json:"subject_id"`
	Count         int    `json:"count"`
	Amount        string `json:"amount"`
}

type entriesResponse struct {
	// Months は、この施策の実績（合計）がある月
	Months []string `json:"months"`
	// Items は明細の1ページ（新しい月から）。Total は絞り込んだ明細の件数（明細を見せない会計科目を除く）
	Items   []entryItem `json:"items"`
	Total   int         `json:"total"`
	Offset  int         `json:"offset"`
	HasMore bool        `json:"has_more"`
	// Hidden は明細を見せない会計科目の合計（絞り込んだ範囲）
	Hidden []hiddenTotal `json:"hidden"`
	// EntriesTotal は明細の合計、FactTotal は実績データ（actual_facts）の合計（絞り込んだ範囲）。
	// 明細のない実績（明細を持たない移行前の実績）があると一致しない
	EntriesTotal string `json:"entries_total"`
	FactTotal    string `json:"fact_total"`
	// RestrictedHidden は、閲覧制限のある科目を除いたか（docs/plan.md「2.17」）
	RestrictedHidden bool `json:"restricted_hidden"`
}

// entriesFilter は明細の絞り込み。where は actual_entries（別名 e）、factWhere は actual_facts の条件。
type entriesFilter struct {
	activityID   int64
	activityCode string
	showDetails  bool // hide_details の会計科目の明細も見せるか（FP&A）
	where        string
	args         []any
	factWhere    string
	factArgs     []any
}

// parseEntriesFilter は month=YYYY-MM・fiscal_year=YYYY・subject_id= を読む。施策がなければ 404。
func (h *Handler) parseEntriesFilter(r *http.Request, u auth.User) (entriesFilter, error) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return entriesFilter{}, err
	}
	f := entriesFilter{activityID: id, showDetails: u.Role == auth.RoleFPAAdmin}
	if err := h.db.QueryRowContext(r.Context(), "SELECT code FROM activities WHERE id = ?", id).Scan(&f.activityCode); errors.Is(err, sql.ErrNoRows) {
		return f, httpx.NotFound("施策が見つかりません")
	} else if err != nil {
		return f, err
	}
	q := r.URL.Query()
	f.where = "e.activity_id = ?" + visibility.SubjectFilter(u, "e.subject_id")
	f.args = []any{id}
	f.factWhere = "activity_id = ?" + visibility.SubjectFilter(u, "subject_id")
	f.factArgs = []any{id}
	add := func(cond string, factCond string, args ...any) {
		f.where += " AND " + cond
		f.args = append(f.args, args...)
		f.factWhere += " AND " + factCond
		f.factArgs = append(f.factArgs, args...)
	}
	if m := q.Get("month"); m != "" {
		if !isYearMonth(m) {
			return f, httpx.BadRequest("month は YYYY-MM 形式で指定してください")
		}
		add("e.target_month = ?", "target_month = ?", m+"-01")
	}
	if s := q.Get("fiscal_year"); s != "" {
		fy, err := strconv.Atoi(s)
		if err != nil || fy < 2000 || fy > 2999 {
			return f, httpx.BadRequest("fiscal_year は年度（例: 2026）で指定してください")
		}
		months := calc.FiscalMonths(fy)
		add("e.target_month BETWEEN ? AND ?", "target_month BETWEEN ? AND ?", months[0]+"-01", months[len(months)-1]+"-01")
	}
	if s := q.Get("subject_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return f, httpx.BadRequest("subject_id は数値で指定してください")
		}
		add("e.subject_id = ?", "subject_id = ?", n)
	}
	return f, nil
}

// detailCond は、明細を行で見せる会計科目の条件（FP&A はすべて）。hidden が true なら見せない会計科目の条件。
func (f entriesFilter) detailCond(hidden bool) string {
	if f.showDetails {
		if hidden {
			return " AND FALSE"
		}
		return ""
	}
	if hidden {
		return " AND g.hide_details"
	}
	return " AND NOT g.hide_details"
}

// activityEntries は GET /api/activities/{id}/actual-entries?month=&fiscal_year=&subject_id=&offset=&limit=。
func (h *Handler) activityEntries(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	f, err := h.parseEntriesFilter(r, u)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	offset, limit := 0, entriesPageSize
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			return httpx.BadRequest("offset は 0 以上の数値で指定してください")
		}
	}
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > entriesMaxPage {
			return httpx.BadRequest(fmt.Sprintf("limit は 1〜%d で指定してください", entriesMaxPage))
		}
	}

	ctx := r.Context()
	resp := entriesResponse{Months: []string{}, Items: []entryItem{}, Hidden: []hiddenTotal{}, Offset: offset}
	if resp.RestrictedHidden, err = visibility.Hidden(ctx, h.db, u); err != nil {
		return err
	}
	if resp.Months, err = h.entryMonths(ctx, u, f.activityID); err != nil {
		return err
	}

	// 件数と合計（絞り込んだ全件）
	if err := h.db.QueryRowContext(ctx, `
		SELECT COUNT(CASE WHEN TRUE`+f.detailCond(false)+` THEN 1 END), CAST(COALESCE(SUM(e.amount), 0) AS CHAR)
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id WHERE `+f.where, f.args...).Scan(&resp.Total, &resp.EntriesTotal); err != nil {
		return err
	}
	if err := h.db.QueryRowContext(ctx,
		"SELECT CAST(COALESCE(SUM(amount), 0) AS CHAR) FROM actual_facts WHERE "+f.factWhere, f.factArgs...).Scan(&resp.FactTotal); err != nil {
		return err
	}

	// 明細を見せない会計科目の合計
	rows, err := h.db.QueryContext(ctx, `
		SELECT g.code, g.name, e.subject_id, COUNT(*), CAST(SUM(e.amount) AS CHAR)
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id
		WHERE `+f.where+f.detailCond(true)+`
		GROUP BY g.code, g.name, e.subject_id ORDER BY g.code, e.subject_id`, f.args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t hiddenTotal
		if err := rows.Scan(&t.GLAccountCode, &t.GLAccountName, &t.SubjectID, &t.Count, &t.Amount); err != nil {
			rows.Close()
			return err
		}
		resp.Hidden = append(resp.Hidden, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// 明細の1ページ（新しい月から）
	rows, err = h.db.QueryContext(ctx, entrySelect+`
		WHERE `+f.where+f.detailCond(false)+`
		ORDER BY e.target_month DESC, g.code, e.line_no, e.id LIMIT ? OFFSET ?`, append(append([]any{}, f.args...), limit, offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return err
		}
		resp.Items = append(resp.Items, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	resp.HasMore = offset+len(resp.Items) < resp.Total
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

const entrySelect = `
		SELECT e.id, DATE_FORMAT(e.target_month, '%Y-%m'), g.code, g.name, e.subject_id,
		       e.department_code, e.box_code, COALESCE(e.description, ''), CAST(e.amount AS CHAR), e.allocated_by
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id`

func scanEntry(rows *sql.Rows) (entryItem, error) {
	var e entryItem
	var dept, box sql.NullString
	if err := rows.Scan(&e.ID, &e.TargetMonth, &e.GLAccountCode, &e.GLAccountName, &e.SubjectID, &dept, &box, &e.Description, &e.Amount, &e.AllocatedBy); err != nil {
		return e, err
	}
	if dept.Valid {
		e.DepartmentCode = &dept.String
	}
	if box.Valid {
		e.BoxCode = &box.String
	}
	return e, nil
}

// entryMonths は施策の実績（合計）がある月（見られる科目だけ）。
func (h *Handler) entryMonths(ctx context.Context, u auth.User, activityID int64) ([]string, error) {
	rows, err := h.db.QueryContext(ctx,
		"SELECT DISTINCT DATE_FORMAT(target_month, '%Y-%m') FROM actual_facts WHERE activity_id = ?"+visibility.SubjectFilter(u, "subject_id")+" ORDER BY 1", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	months := []string{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		months = append(months, m)
	}
	return months, rows.Err()
}

var allocatedByLabels = map[string]string{
	"activity_code": "施策コード",
	"external_code": "外部コード",
	"rule":          "割当ルール",
	"manual":        "未割当の一覧から選択",
	"unallocated":   "未割当",
}

// exportEntries は GET /api/activities/{id}/actual-entries/export?month=&fiscal_year=&subject_id=。
// 絞り込みどおりの明細を、件数の制限なしで CSV（BOM 付き UTF-8）にする。新しい月から並べる。
// 明細を見せない会計科目は、月 × 会計科目 × 科目の合計の行にする（摘要に「明細は FP&A のみ」と書く）。
func (h *Handler) exportEntries(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	f, err := h.parseEntriesFilter(r, u)
	if err != nil {
		return err
	}
	ctx := r.Context()
	subjects := map[int64][2]string{}
	srows, err := h.db.QueryContext(ctx, "SELECT id, code, name FROM subjects")
	if err != nil {
		return err
	}
	for srows.Next() {
		var id int64
		var code, name string
		if err := srows.Scan(&id, &code, &name); err != nil {
			srows.Close()
			return err
		}
		subjects[id] = [2]string{code, name}
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return err
	}

	type line struct {
		month string
		row   []string
	}
	var lines []line
	rows, err := h.db.QueryContext(ctx, entrySelect+`
		WHERE `+f.where+f.detailCond(false)+`
		ORDER BY e.target_month DESC, g.code, e.line_no, e.id`, f.args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			rows.Close()
			return err
		}
		s := subjects[e.SubjectID]
		lines = append(lines, line{e.TargetMonth, []string{e.TargetMonth, s[0], s[1], e.GLAccountCode, e.GLAccountName, deref(e.DepartmentCode), deref(e.BoxCode), e.Description, e.Amount, allocatedByLabels[e.AllocatedBy]}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = h.db.QueryContext(ctx, `
		SELECT DATE_FORMAT(e.target_month, '%Y-%m'), g.code, g.name, e.subject_id, COUNT(*), CAST(SUM(e.amount) AS CHAR)
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id
		WHERE `+f.where+f.detailCond(true)+`
		GROUP BY e.target_month, g.code, g.name, e.subject_id ORDER BY e.target_month DESC, g.code`, f.args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var month, code, name, amount string
		var subjectID int64
		var count int
		if err := rows.Scan(&month, &code, &name, &subjectID, &count, &amount); err != nil {
			rows.Close()
			return err
		}
		s := subjects[subjectID]
		lines = append(lines, line{month, []string{month, s[0], s[1], code, name, "", "", fmt.Sprintf("明細は FP&A のみ（%d 行の合計）", count), amount, ""}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	// 明細と合計の行を、新しい月の順にそろえる（同じ月の中では明細が先）
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].month > lines[j].month })
	out := make([][]string, len(lines))
	for i, l := range lines {
		out[i] = l.row
	}
	header := []string{"target_month", "subject_code", "subject_name", "account_code", "account_name", "department_code", "box_code", "description", "amount", "allocated_by"}
	return csvio.WriteCSV(w, "actual_entries_"+f.activityCode, header, out)
}
