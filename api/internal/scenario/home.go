package scenario

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/target"
	"github.com/rnapyzz/f-panda-app/api/internal/visibility"
)

// ホーム（docs/plan.md「2.10 現場担当の動線」）。シナリオでの施策ごとの状態と、基準・前回見込との差を返す。

// plTotals は年間の収益・費用（円、文字列）。
type plTotals struct {
	Revenue string `json:"revenue"`
	Expense string `json:"expense"`
}

// scenarioRef は比較に使うシナリオ。
type scenarioRef struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	PlanRole      *string `json:"plan_role"`
	ActualThrough *string `json:"actual_through"`
}

// accuracy は、新しく実績になった月の、前回見込の計画値と実績の差（見込の当たり具合）。
type accuracy struct {
	Plan   plTotals `json:"plan"`
	Actual plTotals `json:"actual"`
	// Rate は 科目 × 月の差の絶対値の合計 ÷ 計画値の絶対値の合計（%、小数1桁）。計画値がなければ null
	Rate *float64 `json:"rate"`
	// Large は差が大きい（20% 以上、または計画値がないのに実績がある）か
	Large bool `json:"large"`
}

// activityStatus はホームの1行。
type activityStatus struct {
	ActivityID     int64      `json:"activity_id"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	UnitID         int64      `json:"unit_id"`
	OwnerUserID    *int64     `json:"owner_user_id"`
	Status         string     `json:"status"`
	HasExplanation bool       `json:"has_explanation"`
	Explanation    string     `json:"explanation"`
	Causes         []string   `json:"causes"`
	IsPriority     bool       `json:"is_priority"`
	IsWatched      bool       `json:"is_watched"`
	LastEditedAt   *time.Time `json:"last_edited_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	Current        plTotals   `json:"current"`
	Base           *plTotals  `json:"base"`
	Initial        *plTotals  `json:"initial"` // 期初計画（マネージャーのサービスの状況）
	Revised        *plTotals  `json:"revised"` // 修正計画
	Previous       *plTotals  `json:"previous"`
	Accuracy       *accuracy  `json:"accuracy"`
}

type activityStatusResponse struct {
	Scenario Scenario     `json:"scenario"`
	Scope    string       `json:"scope"`
	Base     *scenarioRef `json:"base"`
	Initial  *scenarioRef `json:"initial"`
	Revised  *scenarioRef `json:"revised"`
	Previous *scenarioRef `json:"previous"`
	// NewActualMonths は、前回見込より新しく実績になった月（実績のお知らせ）
	NewActualMonths []string         `json:"new_actual_months"`
	Items           []activityStatus `json:"items"`
	// RestrictedHidden は、閲覧制限のある科目を除いた金額か（docs/plan.md「2.17」）
	RestrictedHidden bool `json:"restricted_hidden"`
}

// activityStatuses は GET /api/scenarios/{id}/activity-status?scope=mine|units|all。
//
// scope の既定は、現場担当は mine（担当施策）、マネージャーは units（所管ユニットの施策と担当施策）、それ以外は all。
// 基準は同じ年度の修正計画（なければ期初計画、なければ最初に作ったシナリオ）、前回見込はシナリオの前回見込。
func (h *Handler) activityStatuses(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = defaultScope(u)
	}
	if !slices.Contains([]string{"mine", "units", "all"}, scope) {
		return httpx.BadRequest("scope は mine / units / all のいずれかを指定してください")
	}

	ctx := r.Context()
	s, err := findScenario(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	resp := activityStatusResponse{Scenario: s, Scope: scope, NewActualMonths: []string{}, Items: []activityStatus{}}
	if resp.RestrictedHidden, err = visibility.Hidden(ctx, h.db, u); err != nil {
		return err
	}
	if resp.Base, err = baseScenarioOf(ctx, h.db, s.FiscalYear); err != nil {
		return err
	}
	if resp.Initial, err = roleScenarioOf(ctx, h.db, s.FiscalYear, "initial"); err != nil {
		return err
	}
	if resp.Revised, err = roleScenarioOf(ctx, h.db, s.FiscalYear, "revised"); err != nil {
		return err
	}
	if s.PreviousScenarioID != nil {
		p, err := findScenario(ctx, h.db, *s.PreviousScenarioID, "")
		if err != nil {
			return err
		}
		resp.Previous = &scenarioRef{ID: p.ID, Name: p.Name, PlanRole: p.PlanRole, ActualThrough: p.ActualThrough}
		// 前回見込の決算確定月より後で、今回の決算確定月以前の月が、新しく実績になった月
		for _, m := range s.ActualMonths() {
			if p.ActualThrough == nil || m > *p.ActualThrough {
				resp.NewActualMonths = append(resp.NewActualMonths, m)
			}
		}
	}

	// 対象の施策（更新の対象、docs/plan.md「2.10」）
	where, args := " WHERE "+target.Condition("a"), []any{id}
	switch scope {
	case "mine":
		where, args = where+" AND a.owner_user_id = ?", append(args, u.ID)
	case "units":
		where, args = where+" AND (un.owner_user_id = ? OR a.owner_user_id = ?)", append(args, u.ID, u.ID)
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT a.id, a.code, a.name, a.unit_id, a.owner_user_id, a.is_priority, w.user_id IS NOT NULL,
		       COALESCE(n.explanation, ''), COALESCE(n.causes, ''), n.last_edited_at, n.completed_at
		FROM activities a
		JOIN units un ON un.id = a.unit_id
		LEFT JOIN activity_scenario_notes n ON n.activity_id = a.id AND n.scenario_id = ?
		LEFT JOIN activity_watches w ON w.activity_id = a.id AND w.user_id = ?`+where+`
		ORDER BY a.code`, append([]any{id, u.ID}, args...)...)
	if err != nil {
		return err
	}
	index := map[int64]int{}
	for rows.Next() {
		var it activityStatus
		var owner sql.NullInt64
		var causes string
		var edited, completed sql.NullTime
		if err := rows.Scan(&it.ActivityID, &it.Code, &it.Name, &it.UnitID, &owner, &it.IsPriority, &it.IsWatched, &it.Explanation, &causes, &edited, &completed); err != nil {
			rows.Close()
			return err
		}
		if owner.Valid {
			it.OwnerUserID = &owner.Int64
		}
		it.HasExplanation = it.Explanation != ""
		it.Causes = []string{}
		if causes != "" {
			it.Causes = strings.Split(causes, ",")
		}
		if edited.Valid {
			it.LastEditedAt = &edited.Time
		}
		if completed.Valid {
			it.CompletedAt = &completed.Time
		}
		switch {
		case it.CompletedAt != nil:
			it.Status = statusCompleted
		case it.LastEditedAt != nil:
			it.Status = statusInProgress
		default:
			it.Status = statusNotStarted
		}
		it.Current = plTotals{Revenue: "0", Expense: "0"}
		index[it.ActivityID] = len(resp.Items)
		resp.Items = append(resp.Items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// 年間の収益・費用（決算確定月以前は実績）。シナリオごとの集計は互いに関係しないので、並行して読む（I-15）
	type fillTarget struct {
		id  int64
		set func(*activityStatus, plTotals)
	}
	targets := []fillTarget{{s.ID, func(it *activityStatus, t plTotals) { it.Current = t }}}
	if resp.Base != nil {
		targets = append(targets, fillTarget{resp.Base.ID, func(it *activityStatus, t plTotals) { it.Base = &t }})
	}
	if resp.Initial != nil {
		targets = append(targets, fillTarget{resp.Initial.ID, func(it *activityStatus, t plTotals) { it.Initial = &t }})
	}
	if resp.Revised != nil {
		targets = append(targets, fillTarget{resp.Revised.ID, func(it *activityStatus, t plTotals) { it.Revised = &t }})
	}
	if resp.Previous != nil {
		targets = append(targets, fillTarget{resp.Previous.ID, func(it *activityStatus, t plTotals) { it.Previous = &t }})
	}
	results := make([]map[int64]plTotals, len(targets))
	errs := make([]error, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = annualTotals(ctx, h.db, u, t.id, nil)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	for i, t := range targets {
		for j := range resp.Items {
			v, ok := results[i][resp.Items[j].ActivityID]
			if !ok {
				v = plTotals{Revenue: "0", Expense: "0"}
			}
			t.set(&resp.Items[j], v)
		}
	}

	// 見込の当たり具合（新しく実績になった月の、前回見込の計画値と実績）
	if resp.Previous != nil && len(resp.NewActualMonths) > 0 {
		acc, err := accuracyOf(ctx, h.db, u, s.ID, resp.Previous.ID, resp.NewActualMonths)
		if err != nil {
			return err
		}
		for aid, a := range acc {
			if i, ok := index[aid]; ok {
				a := a
				resp.Items[i].Accuracy = &a
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func defaultScope(u auth.User) string {
	switch u.Role {
	case auth.RoleMember:
		return "mine"
	case auth.RoleManager:
		return "units"
	default:
		return "all"
	}
}

// baseScenarioOf は年度の基準シナリオ（修正計画、なければ期初計画、なければ最初に作ったシナリオ）を返す。
func baseScenarioOf(ctx context.Context, db *sql.DB, fiscalYear int) (*scenarioRef, error) {
	s, err := scanScenario(db.QueryRowContext(ctx, scenarioSelect+`
		WHERE fiscal_year = ?
		ORDER BY CASE plan_role WHEN 'revised' THEN 0 WHEN 'initial' THEN 1 ELSE 2 END, id LIMIT 1`, fiscalYear))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &scenarioRef{ID: s.ID, Name: s.Name, PlanRole: s.PlanRole, ActualThrough: s.ActualThrough}, nil
}

// roleScenarioOf は年度でエイリアス（initial / revised）を持つシナリオを返す（なければ nil）。
func roleScenarioOf(ctx context.Context, db *sql.DB, fiscalYear int, role string) (*scenarioRef, error) {
	s, err := scanScenario(db.QueryRowContext(ctx, scenarioSelect+" WHERE fiscal_year = ? AND plan_role = ?", fiscalYear, role))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &scenarioRef{ID: s.ID, Name: s.Name, PlanRole: s.PlanRole, ActualThrough: s.ActualThrough}, nil
}

// annualTotals はシナリオの施策ごとの収益・費用の合計（months を指定すればその月だけ）を返す。
// ユーザーが見られない科目（閲覧制限）は除く。
func annualTotals(ctx context.Context, db *sql.DB, u auth.User, scenarioID int64, months []string) (map[int64]plTotals, error) {
	query := `
		SELECT b.activity_id,
		       CAST(COALESCE(SUM(CASE WHEN s.category = 'revenue' THEN b.amount END), 0) AS CHAR),
		       CAST(COALESCE(SUM(CASE WHEN s.category = 'expense' THEN b.amount END), 0) AS CHAR)
		FROM scenario_amounts b JOIN subjects s ON s.id = b.subject_id
		WHERE b.scenario_id = ?` + visibility.SubjectFilter(u, "b.subject_id")
	args := []any{scenarioID}
	if len(months) > 0 {
		query += " AND b.target_month BETWEEN ? AND ?"
		args = append(args, months[0]+"-01", months[len(months)-1]+"-01")
	}
	rows, err := db.QueryContext(ctx, query+" GROUP BY b.activity_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]plTotals{}
	for rows.Next() {
		var id int64
		var t plTotals
		if err := rows.Scan(&id, &t.Revenue, &t.Expense); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// accuracyOf は、months（新しく実績になった月）について、施策ごとに前回見込の計画値と今回の実績を比べる。
// 差の率は、科目 × 月の差の絶対値の合計 ÷ 前回見込の絶対値の合計（docs/plan.md「2.8」の見込の当たり具合）。
// ユーザーが見られない科目（閲覧制限）は除く。
func accuracyOf(ctx context.Context, db *sql.DB, u auth.User, currentID, previousID int64, months []string) (map[int64]accuracy, error) {
	type key struct {
		activityID, subjectID int64
		month                 string
	}
	load := func(scenarioID int64) (map[key]*big.Int, error) {
		rows, err := db.QueryContext(ctx, `
			SELECT activity_id, subject_id, DATE_FORMAT(target_month, '%Y-%m'), CAST(SUM(amount) AS CHAR)
			FROM scenario_amounts WHERE scenario_id = ? AND target_month BETWEEN ? AND ?`+visibility.SubjectFilter(u, "subject_id")+`
			GROUP BY activity_id, subject_id, target_month`, scenarioID, months[0]+"-01", months[len(months)-1]+"-01")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[key]*big.Int{}
		for rows.Next() {
			var k key
			var amount string
			if err := rows.Scan(&k.activityID, &k.subjectID, &k.month, &amount); err != nil {
				return nil, err
			}
			v, _ := new(big.Int).SetString(amount, 10)
			out[k] = v
		}
		return out, rows.Err()
	}
	actual, err := load(currentID)
	if err != nil {
		return nil, err
	}
	plan, err := load(previousID)
	if err != nil {
		return nil, err
	}
	planTotals, err := annualTotals(ctx, db, u, previousID, months)
	if err != nil {
		return nil, err
	}
	actualTotals, err := annualTotals(ctx, db, u, currentID, months)
	if err != nil {
		return nil, err
	}

	diffSum := map[int64]*big.Int{}
	planSum := map[int64]*big.Int{}
	add := func(m map[int64]*big.Int, id int64, v *big.Int) {
		if m[id] == nil {
			m[id] = new(big.Int)
		}
		m[id].Add(m[id], v)
	}
	keys := map[key]bool{}
	for k := range actual {
		keys[k] = true
	}
	for k := range plan {
		keys[k] = true
	}
	for k := range keys {
		a, p := actual[k], plan[k]
		if a == nil {
			a = new(big.Int)
		}
		if p == nil {
			p = new(big.Int)
		}
		add(diffSum, k.activityID, new(big.Int).Abs(new(big.Int).Sub(a, p)))
		add(planSum, k.activityID, new(big.Int).Abs(p))
	}

	zero := plTotals{Revenue: "0", Expense: "0"}
	out := map[int64]accuracy{}
	for id, d := range diffSum {
		acc := accuracy{Plan: zero, Actual: zero}
		if t, ok := planTotals[id]; ok {
			acc.Plan = t
		}
		if t, ok := actualTotals[id]; ok {
			acc.Actual = t
		}
		p := planSum[id]
		if p == nil || p.Sign() == 0 {
			acc.Large = d.Sign() != 0
		} else {
			rate, _ := new(big.Rat).SetFrac(new(big.Int).Mul(d, big.NewInt(1000)), p).Float64()
			r := float64(int64(rate+0.5)) / 10 // 0.1% 単位に四捨五入
			acc.Rate = &r
			// 20% 以上: 差 × 5 ≥ 計画値
			acc.Large = new(big.Int).Mul(d, big.NewInt(5)).Cmp(p) >= 0
		}
		out[id] = acc
	}
	return out, nil
}
