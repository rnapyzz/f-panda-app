package actual

import (
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 施策の実績の明細（docs/plan.md「2.12」の実績の明細）。

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
	Month string `json:"month"`
	// Months は、この施策の実績（合計）がある月
	Months []string      `json:"months"`
	Items  []entryItem   `json:"items"`
	Hidden []hiddenTotal `json:"hidden"`
	// EntriesTotal は明細の合計、FactTotal は実績データ（actual_facts）の合計。
	// 明細のない実績（明細を持たない移行前の実績）があると一致しない
	EntriesTotal string `json:"entries_total"`
	FactTotal    string `json:"fact_total"`
}

// activityEntries は GET /api/activities/{id}/actual-entries?month=YYYY-MM&subject_id=。
// 施策に割り当てた明細を返す。hide_details の会計科目は、FP&A 以外には会計科目ごとの合計だけを返す。
// month を省略すると、実績がある最後の月。
func (h *Handler) activityEntries(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var subjectID *int64
	if s := q.Get("subject_id"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest("subject_id は数値で指定してください")
		}
		subjectID = &n
	}
	ctx := r.Context()
	var one int
	if err := h.db.QueryRowContext(ctx, "SELECT 1 FROM activities WHERE id = ?", id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return httpx.NotFound("施策が見つかりません")
	} else if err != nil {
		return err
	}

	resp := entriesResponse{Month: q.Get("month"), Months: []string{}, Items: []entryItem{}, Hidden: []hiddenTotal{}}
	rows, err := h.db.QueryContext(ctx,
		"SELECT DISTINCT DATE_FORMAT(target_month, '%Y-%m') FROM actual_facts WHERE activity_id = ? ORDER BY 1", id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return err
		}
		resp.Months = append(resp.Months, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if resp.Month == "" {
		if len(resp.Months) == 0 {
			resp.EntriesTotal, resp.FactTotal = "0", "0"
			httpx.WriteJSON(w, http.StatusOK, resp)
			return nil
		}
		resp.Month = resp.Months[len(resp.Months)-1]
	}
	if !isYearMonth(resp.Month) {
		return httpx.BadRequest("month は YYYY-MM 形式で指定してください")
	}

	where, args := "e.activity_id = ? AND e.target_month = ?", []any{id, resp.Month + "-01"}
	factWhere, factArgs := "activity_id = ? AND target_month = ?", []any{id, resp.Month + "-01"}
	if subjectID != nil {
		where += " AND e.subject_id = ?"
		args = append(args, *subjectID)
		factWhere += " AND subject_id = ?"
		factArgs = append(factArgs, *subjectID)
	}
	rows, err = h.db.QueryContext(ctx, `
		SELECT e.id, DATE_FORMAT(e.target_month, '%Y-%m'), g.code, g.name, g.hide_details, e.subject_id,
		       e.department_code, e.box_code, COALESCE(e.description, ''), CAST(e.amount AS CHAR), e.allocated_by
		FROM actual_entries e JOIN gl_accounts g ON g.id = e.gl_account_id
		WHERE `+where+` ORDER BY g.code, e.line_no`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	showAll := u.Role == auth.RoleFPAAdmin
	total := new(big.Int)
	type hiddenKey struct {
		account string
		subject int64
	}
	hidden := map[hiddenKey]*big.Int{}
	for rows.Next() {
		var e entryItem
		var hide bool
		var dept, box sql.NullString
		if err := rows.Scan(&e.ID, &e.TargetMonth, &e.GLAccountCode, &e.GLAccountName, &hide, &e.SubjectID,
			&dept, &box, &e.Description, &e.Amount, &e.AllocatedBy); err != nil {
			return err
		}
		v, _ := new(big.Int).SetString(e.Amount, 10)
		total.Add(total, v)
		if hide && !showAll {
			// 行は見せず、会計科目 × 科目の合計にまとめる（行は会計科目の順に並んでいる）
			k := hiddenKey{e.GLAccountCode, e.SubjectID}
			if hidden[k] == nil {
				hidden[k] = new(big.Int)
				resp.Hidden = append(resp.Hidden, hiddenTotal{GLAccountCode: e.GLAccountCode, GLAccountName: e.GLAccountName, SubjectID: e.SubjectID})
			}
			hidden[k].Add(hidden[k], v)
			for i := range resp.Hidden {
				if t := &resp.Hidden[i]; t.GLAccountCode == k.account && t.SubjectID == k.subject {
					t.Count++
					t.Amount = hidden[k].String()
				}
			}
			continue
		}
		if dept.Valid {
			e.DepartmentCode = &dept.String
		}
		if box.Valid {
			e.BoxCode = &box.String
		}
		resp.Items = append(resp.Items, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	resp.EntriesTotal = total.String()
	if err := h.db.QueryRowContext(ctx,
		"SELECT CAST(COALESCE(SUM(amount), 0) AS CHAR) FROM actual_facts WHERE "+factWhere, factArgs...).Scan(&resp.FactTotal); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}
