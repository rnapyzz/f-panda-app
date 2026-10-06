package actual

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// ロック済みのシナリオの実績と、今の実績の食い違い（docs/plan.md「2.14」）。
// ロック済みのシナリオはロック時の実績（scenario_actuals）を固定しているので、後から会計側の修正を取り込むとずれる。

// queryer は *sql.DB と *sql.Tx に共通する問い合わせのメソッド。
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// driftMonth は1か月の食い違い。差は「今の実績 − シナリオに保存した実績」（未割当を含む）。
// Changed は施策 × 科目で金額が違う件数（施策の間の付け替えだけなら、収益・費用の差は 0 でも Changed は 1 以上）。
type driftMonth struct {
	Month   string `json:"month"`
	Revenue string `json:"revenue"`
	Expense string `json:"expense"`
	Changed int    `json:"changed"`
}

type scenarioDrift struct {
	ScenarioID    int64        `json:"scenario_id"`
	Name          string       `json:"name"`
	FiscalYear    int          `json:"fiscal_year"`
	ActualThrough string       `json:"actual_through"`
	Months        []driftMonth `json:"months"`
}

// loadDrift はロック済みのシナリオごとの食い違いを返す。scenarioID を指定するとそのシナリオだけ、
// months を指定するとその月だけを比べる。食い違いのないシナリオは返さない。
func loadDrift(ctx context.Context, q queryer, scenarioID int64, months []string) ([]scenarioDrift, error) {
	snapWhere, factWhere, args := "", "", []any{}
	if scenarioID != 0 {
		snapWhere += " AND s.id = ?"
		args = append(args, scenarioID)
	}
	if len(months) > 0 {
		snapWhere += " AND sa.target_month IN (" + placeholders(len(months)) + ")"
		args = append(args, monthArgs(months)...)
	}
	if scenarioID != 0 {
		factWhere += " AND s.id = ?"
		args = append(args, scenarioID)
	}
	if len(months) > 0 {
		factWhere += " AND af.target_month IN (" + placeholders(len(months)) + ")"
		args = append(args, monthArgs(months)...)
	}
	rows, err := q.QueryContext(ctx, `
		SELECT x.sid, s.name, s.fiscal_year, DATE_FORMAT(s.actual_through, '%Y-%m'), x.m, sub.category, CAST(SUM(x.amt) AS CHAR)
		FROM (
			SELECT sa.scenario_id AS sid, DATE_FORMAT(sa.target_month, '%Y-%m') AS m, sa.activity_key AS akey, sa.subject_id AS subject_id, -sa.amount AS amt
			FROM scenario_actuals sa JOIN scenarios s ON s.id = sa.scenario_id
			WHERE s.is_locked`+snapWhere+`
			UNION ALL
			SELECT s.id, DATE_FORMAT(af.target_month, '%Y-%m'), af.activity_key, af.subject_id, af.amount
			FROM scenarios s
			JOIN actual_facts af ON af.target_month BETWEEN MAKEDATE(s.fiscal_year, 1) + INTERVAL 3 MONTH AND s.actual_through
			WHERE s.is_locked AND s.actual_through IS NOT NULL`+factWhere+`
		) x
		JOIN scenarios s ON s.id = x.sid
		JOIN subjects sub ON sub.id = x.subject_id
		GROUP BY x.sid, s.name, s.fiscal_year, s.actual_through, x.m, x.akey, x.subject_id, sub.category
		HAVING SUM(x.amt) <> 0
		ORDER BY x.sid, x.m`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type monthSum struct {
		revenue, expense *big.Int
		changed          int
	}
	byScenario := map[int64]*scenarioDrift{}
	sums := map[int64]map[string]*monthSum{}
	var order []int64
	for rows.Next() {
		var id int64
		var name, through, month, category, amount string
		var fy int
		if err := rows.Scan(&id, &name, &fy, &through, &month, &category, &amount); err != nil {
			return nil, err
		}
		if byScenario[id] == nil {
			byScenario[id] = &scenarioDrift{ScenarioID: id, Name: name, FiscalYear: fy, ActualThrough: through, Months: []driftMonth{}}
			sums[id] = map[string]*monthSum{}
			order = append(order, id)
		}
		ms := sums[id][month]
		if ms == nil {
			ms = &monthSum{revenue: new(big.Int), expense: new(big.Int)}
			sums[id][month] = ms
		}
		v, _ := new(big.Int).SetString(amount, 10)
		if category == "revenue" {
			ms.revenue.Add(ms.revenue, v)
		} else {
			ms.expense.Add(ms.expense, v)
		}
		ms.changed++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []scenarioDrift{}
	for _, id := range order {
		d := byScenario[id]
		var ms []string
		for m := range sums[id] {
			ms = append(ms, m)
		}
		sort.Strings(ms)
		for _, m := range ms {
			s := sums[id][m]
			d.Months = append(d.Months, driftMonth{Month: m, Revenue: s.revenue.String(), Expense: s.expense.String(), Changed: s.changed})
		}
		out = append(out, *d)
	}
	return out, nil
}

// listDrift は GET /api/scenarios/actual-drift。FP&A のみ。
func (h *Handler) listDrift(w http.ResponseWriter, r *http.Request) error {
	items, err := loadDrift(r.Context(), h.db, 0, nil)
	if err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

type refreshRequest struct {
	Reason string `json:"reason"`
	DryRun bool   `json:"dry_run"`
}

type refreshResult struct {
	DryRun bool          `json:"dry_run"`
	Drift  scenarioDrift `json:"drift"`
	// Saved は入れ替えた後に保存した実績の件数
	Saved int64 `json:"saved"`
}

// refreshActuals は POST /api/scenarios/{id}/refresh-actuals。FP&A のみ。
// ロック済みのシナリオに保存した決算確定月以前の実績（未割当を含む）を、今の実績に入れ替える。ロックは外れない。
// dry_run なら保存せず、月ごとの差だけを返す。
func (h *Handler) refreshActuals(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if !req.DryRun && strings.TrimSpace(req.Reason) == "" {
		return httpx.Validation(map[string]string{"reason": "実績を最新にするには変更理由の入力が必要です"})
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	result := refreshResult{DryRun: req.DryRun}
	err = audit.InTx(ctx, h.db, u.ID, &id, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		var name string
		var fy int
		var locked bool
		var through sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT name, fiscal_year, is_locked, DATE_FORMAT(actual_through, '%Y-%m') FROM scenarios WHERE id = ? FOR UPDATE", id).
			Scan(&name, &fy, &locked, &through)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("シナリオが見つかりません")
		}
		if err != nil {
			return err
		}
		if !locked || !through.Valid {
			return httpx.Conflict("実績を最新にできるのは、決算確定月のあるロック済みのシナリオだけです")
		}
		drift, err := loadDrift(ctx, tx, id, nil)
		if err != nil {
			return err
		}
		if len(drift) == 0 {
			return httpx.Conflict(fmt.Sprintf("「%s」の実績は最新の実績と同じです", name))
		}
		result.Drift = drift[0]
		if req.DryRun {
			return errDryRun
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM scenario_actuals WHERE scenario_id = ?", id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO scenario_actuals (scenario_id, activity_id, subject_id, target_month, amount)
			SELECT ?, activity_id, subject_id, target_month, amount FROM actual_facts
			WHERE target_month BETWEEN ? AND ?`,
			id, calc.FiscalMonths(fy)[0]+"-01", through.String+"-01")
		if err != nil {
			return err
		}
		result.Saved, _ = res.RowsAffected()
		// 入れ替えた件数と月ごとの差を記録する（行ごとの監査ログは actual_facts に残っている）
		return rec.Update(ctx, "scenarios", id, nil, struct {
			ID               int64        `json:"id"`
			Name             string       `json:"name"`
			RefreshedActuals int64        `json:"refreshed_actuals"`
			Drift            []driftMonth `json:"actual_drift"`
		}{id, name, result.Saved, result.Drift.Months})
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}
