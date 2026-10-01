package report

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const (
	// upcomingDays は「期日が近い」とみなすマイルストーンの日数
	upcomingDays = 30
	// maxProvisionalReasons は施策ごとに返す仮の値の理由の数
	maxProvisionalReasons = 5
)

// pl は収益と費用の合計（円、文字列）。
type pl struct {
	Revenue string `json:"revenue"`
	Expense string `json:"expense"`
}

type riskMilestone struct {
	Name    string `json:"name"`
	DueDate string `json:"due_date"`
	Status  string `json:"status"`
	// Risk は overdue（期日超過）/ delayed（遅延）/ upcoming（期日が近い）
	Risk string `json:"risk"`
}

type riskProvisional struct {
	Count   int      `json:"count"`
	Revenue string   `json:"revenue"`
	Expense string   `json:"expense"`
	Reasons []string `json:"reasons"`
}

// RiskActivity は施策ごとのリスク情報。
type RiskActivity struct {
	ID           int64        `json:"id"`
	Code         string       `json:"code"`
	Name         string       `json:"name"`
	UnitID       int64        `json:"unit_id"`
	OwnerUserID  *int64       `json:"owner_user_id"`
	ActivityType string       `json:"activity_type"`
	Status       string       `json:"status"`
	Probability  *json.Number `json:"probability"`
	Assumptions  string       `json:"assumptions"`

	Base        pl  `json:"base"`
	Optimistic  *pl `json:"optimistic"`
	Pessimistic *pl `json:"pessimistic"`

	Provisional riskProvisional `json:"provisional"`
	Milestones  []riskMilestone `json:"milestones"`
	// Conditions はシナリオごとの想定条件（base / optimistic / pessimistic）
	Conditions map[string]string `json:"conditions"`
}

type riskScenario struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	PlanRole *string `json:"plan_role"`
}

type riskResponse struct {
	FiscalYear  int            `json:"fiscal_year"`
	Today       string         `json:"today"`
	Base        riskScenario   `json:"base"`
	Optimistic  *riskScenario  `json:"optimistic"`
	Pessimistic *riskScenario  `json:"pessimistic"`
	Activities  []RiskActivity `json:"activities"`
}

// risk は GET /api/reports/risk。
//
// クエリパラメーター:
//   - scenario_id: 基準のシナリオ（必須。通常は最新の見込）
//   - optimistic_id / pessimistic_id: 楽観・悲観として比べるシナリオ（任意、基準と同じ年度）
//
// 施策ごとに、確度・前提条件、基準／楽観／悲観の年間の収益・費用、基準の仮の値、
// 注意が必要なマイルストーン（期日超過・遅延・30日以内に期日）、想定条件を返す。
func (h *Handler) risk(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()

	ids := map[string]int64{}
	for _, p := range []string{"scenario_id", "optimistic_id", "pessimistic_id"} {
		s := q.Get(p)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest(p + " は数値で指定してください")
		}
		ids[p] = id
	}
	if _, ok := ids["scenario_id"]; !ok {
		return httpx.Validation(map[string]string{"scenario_id": "基準のシナリオを選んでください"})
	}

	resp := riskResponse{Activities: []RiskActivity{}}
	// 基準を先に読み込み、楽観・悲観は基準と同じ年度であることを確認する
	for _, p := range []string{"scenario_id", "optimistic_id", "pessimistic_id"} {
		id, ok := ids[p]
		if !ok {
			continue
		}
		var s riskScenario
		var fy int
		var role sql.NullString
		err := h.db.QueryRowContext(ctx, "SELECT id, name, plan_role, fiscal_year FROM scenarios WHERE id = ?", id).Scan(&s.ID, &s.Name, &role, &fy)
		if role.Valid {
			s.PlanRole = &role.String
		}
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.Validation(map[string]string{p: fmt.Sprintf("シナリオ %d が見つかりません", id)})
		}
		if err != nil {
			return err
		}
		switch p {
		case "scenario_id":
			resp.FiscalYear = fy
			resp.Base = s
		default:
			if fy != resp.FiscalYear {
				return httpx.Validation(map[string]string{p: "基準と同じ年度のシナリオを選んでください"})
			}
			if p == "optimistic_id" {
				resp.Optimistic = &s
			} else {
				resp.Pessimistic = &s
			}
		}
	}

	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		jst = time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	today := time.Now().In(jst)
	resp.Today = today.Format("2006-01-02")

	activities, index, err := loadRiskActivities(ctx, h.db)
	if err != nil {
		return err
	}

	// シナリオごとの年間の収益・費用
	for p, target := range map[string]func(a *RiskActivity, v pl){
		"scenario_id":    func(a *RiskActivity, v pl) { a.Base = v },
		"optimistic_id":  func(a *RiskActivity, v pl) { v2 := v; a.Optimistic = &v2 },
		"pessimistic_id": func(a *RiskActivity, v pl) { v2 := v; a.Pessimistic = &v2 },
	} {
		id, ok := ids[p]
		if !ok {
			continue
		}
		totals, err := activityTotals(ctx, h.db, id, false)
		if err != nil {
			return err
		}
		for i := range activities {
			target(&activities[i], orZero(totals, activities[i].ID))
		}
	}

	// 基準シナリオの仮の値
	provisional, err := activityTotals(ctx, h.db, ids["scenario_id"], true)
	if err != nil {
		return err
	}
	counts, reasons, err := provisionalDetails(ctx, h.db, ids["scenario_id"])
	if err != nil {
		return err
	}
	for i := range activities {
		a := &activities[i]
		p := orZero(provisional, a.ID)
		a.Provisional = riskProvisional{Count: counts[a.ID], Revenue: p.Revenue, Expense: p.Expense, Reasons: reasons[a.ID]}
		if a.Provisional.Reasons == nil {
			a.Provisional.Reasons = []string{}
		}
	}

	// マイルストーン
	if err := fillRiskMilestones(ctx, h.db, index, today); err != nil {
		return err
	}

	// 想定条件
	for key, p := range map[string]string{"base": "scenario_id", "optimistic": "optimistic_id", "pessimistic": "pessimistic_id"} {
		id, ok := ids[p]
		if !ok {
			continue
		}
		rows, err := h.db.QueryContext(ctx, "SELECT activity_id, description FROM scenario_conditions WHERE scenario_id = ?", id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var aid int64
			var desc string
			if err := rows.Scan(&aid, &desc); err != nil {
				rows.Close()
				return err
			}
			if a, ok := index[aid]; ok {
				a.Conditions[key] = desc
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	resp.Activities = activities
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func loadRiskActivities(ctx context.Context, db *sql.DB) ([]RiskActivity, map[int64]*RiskActivity, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, code, name, unit_id, owner_user_id, activity_type, status, probability, COALESCE(assumptions, '')
		FROM activities ORDER BY code`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []RiskActivity
	for rows.Next() {
		var a RiskActivity
		var owner sql.NullInt64
		var prob sql.NullString
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.UnitID, &owner, &a.ActivityType, &a.Status, &prob, &a.Assumptions); err != nil {
			return nil, nil, err
		}
		if owner.Valid {
			o := owner.Int64
			a.OwnerUserID = &o
		}
		if prob.Valid {
			n := json.Number(prob.String)
			a.Probability = &n
		}
		a.Base = pl{Revenue: "0", Expense: "0"}
		a.Milestones = []riskMilestone{}
		a.Conditions = map[string]string{}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if out == nil {
		out = []RiskActivity{}
	}
	index := map[int64]*RiskActivity{}
	for i := range out {
		index[out[i].ID] = &out[i]
	}
	return out, index, nil
}

// activityTotals はシナリオの施策ごとの年間の収益・費用（決算確定月以前は実績）を返す。provisionalOnly なら仮の値だけを合計する。
func activityTotals(ctx context.Context, db *sql.DB, scenarioID int64, provisionalOnly bool) (map[int64]pl, error) {
	where := ""
	if provisionalOnly {
		where = " AND b.is_provisional"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT b.activity_id,
		       CAST(COALESCE(SUM(CASE WHEN s.category = 'revenue' THEN b.amount END), 0) AS CHAR),
		       CAST(COALESCE(SUM(CASE WHEN s.category = 'expense' THEN b.amount END), 0) AS CHAR)
		FROM scenario_amounts b JOIN subjects s ON s.id = b.subject_id
		WHERE b.scenario_id = ?`+where+`
		GROUP BY b.activity_id`, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]pl{}
	for rows.Next() {
		var id int64
		var v pl
		if err := rows.Scan(&id, &v.Revenue, &v.Expense); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// provisionalDetails は施策ごとの仮の値の件数と理由（重複を除き最大5件）を返す。
func provisionalDetails(ctx context.Context, db *sql.DB, scenarioID int64) (map[int64]int, map[int64][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT activity_id, COALESCE(provisional_reason, '')
		FROM scenario_amounts WHERE scenario_id = ? AND is_provisional
		ORDER BY activity_id, target_month`, scenarioID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	counts := map[int64]int{}
	reasons := map[int64][]string{}
	seen := map[int64]map[string]bool{}
	for rows.Next() {
		var id int64
		var reason string
		if err := rows.Scan(&id, &reason); err != nil {
			return nil, nil, err
		}
		counts[id]++
		if reason == "" || seen[id][reason] || len(reasons[id]) >= maxProvisionalReasons {
			continue
		}
		if seen[id] == nil {
			seen[id] = map[string]bool{}
		}
		seen[id][reason] = true
		reasons[id] = append(reasons[id], reason)
	}
	return counts, reasons, rows.Err()
}

// fillRiskMilestones は完了していないマイルストーンのうち、期日超過・遅延・期日が近いものを施策に加える。
func fillRiskMilestones(ctx context.Context, db *sql.DB, index map[int64]*RiskActivity, today time.Time) error {
	todayStr := today.Format("2006-01-02")
	soon := today.AddDate(0, 0, upcomingDays).Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `
		SELECT activity_id, name, DATE_FORMAT(due_date, '%Y-%m-%d'), status
		FROM activity_milestones
		WHERE status <> 'completed' AND (status = 'delayed' OR due_date <= ?)
		ORDER BY due_date, id`, soon)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var m riskMilestone
		if err := rows.Scan(&aid, &m.Name, &m.DueDate, &m.Status); err != nil {
			return err
		}
		switch {
		case m.DueDate < todayStr:
			m.Risk = "overdue"
		case m.Status == "delayed":
			m.Risk = "delayed"
		default:
			m.Risk = "upcoming"
		}
		if a, ok := index[aid]; ok {
			a.Milestones = append(a.Milestones, m)
		}
	}
	return rows.Err()
}

// orZero は施策の合計を返す。金額がなければ 0。
func orZero(m map[int64]pl, id int64) pl {
	if v, ok := m[id]; ok {
		return v
	}
	return pl{Revenue: "0", Expense: "0"}
}
