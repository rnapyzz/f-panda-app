package server_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func riskActivity(body map[string]any, code string) map[string]any {
	for _, a := range body["activities"].([]any) {
		if m := a.(map[string]any); m["code"] == code {
			return m
		}
	}
	return nil
}

func TestRiskReport(t *testing.T) {
	f := newScenarioFixture(t)
	amounts := func(sid int64, items ...map[string]any) {
		t.Helper()
		if status, body := f.admin.do("PUT", f.valuesPath(sid, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": items}); status != http.StatusOK {
			t.Fatalf("amounts: status = %d, body = %v", status, body)
		}
	}
	amounts(f.budget,
		map[string]any{"subject_id": f.sales, "target_month": "2026-04", "amount": 1000},
		map[string]any{"subject_id": f.sales, "target_month": "2026-05", "amount": 500, "is_provisional": true, "provisional_reason": "受注待ち"},
		map[string]any{"subject_id": f.cost, "target_month": "2026-05", "amount": 200, "is_provisional": true, "provisional_reason": "見積待ち"},
	)
	opt := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "楽観", "fiscal_year": 2026, "base_scenario_id": f.budget})
	pes := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "悲観", "fiscal_year": 2026, "base_scenario_id": f.budget})
	amounts(opt, map[string]any{"subject_id": f.sales, "target_month": "2026-06", "amount": 800})
	amounts(pes, map[string]any{"subject_id": f.sales, "target_month": "2026-05", "amount": nil})
	f.admin.do("PUT", f.valuesPath(opt, f.manualAct)+"/condition", map[string]any{"description": "追加発注が確定した場合"})

	// マイルストーン: 期日超過・遅延・期日が近い・完了（対象外）・先の予定（対象外）
	today := time.Now().In(time.FixedZone("JST", 9*60*60))
	ms := fmt.Sprintf("/api/activities/%d/milestones", f.manualAct)
	for _, m := range []map[string]any{
		{"name": "期日超過", "due_date": "2020-01-01", "status": "in_progress"},
		{"name": "遅延", "due_date": "2099-01-01", "status": "delayed"},
		{"name": "期日が近い", "due_date": today.AddDate(0, 0, 5).Format("2006-01-02"), "status": "not_started"},
		{"name": "完了済み", "due_date": "2020-01-01", "status": "completed"},
		{"name": "先の予定", "due_date": today.AddDate(0, 0, 60).Format("2006-01-02"), "status": "not_started"},
	} {
		f.member.mustCreate(ms, m)
	}

	body := f.viewer.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d&optimistic_id=%d&pessimistic_id=%d", f.budget, opt, pes))
	a := riskActivity(body, "PRJ-1")
	if a == nil {
		t.Fatalf("PRJ-1 がない: %v", body)
	}
	check := func(name string, got, want any) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("基準の収益", a["base"].(map[string]any)["revenue"], "1500")
	check("基準の費用", a["base"].(map[string]any)["expense"], "200")
	check("楽観の収益", a["optimistic"].(map[string]any)["revenue"], "2300")
	check("悲観の収益", a["pessimistic"].(map[string]any)["revenue"], "1000")
	p := a["provisional"].(map[string]any)
	check("仮の値の件数", p["count"], 2)
	check("仮の値の収益", p["revenue"], "500")
	check("仮の値の理由", p["reasons"], []any{"受注待ち", "見積待ち"})
	check("楽観の想定条件", a["conditions"].(map[string]any)["optimistic"], "追加発注が確定した場合")

	var got []string
	for _, m := range a["milestones"].([]any) {
		mm := m.(map[string]any)
		got = append(got, mm["name"].(string)+":"+mm["risk"].(string))
	}
	check("マイルストーン", got, []string{"期日超過:overdue", "期日が近い:upcoming", "遅延:delayed"})

	// 金額のない施策も 0 で返る
	other := riskActivity(body, "SAAS-1")
	check("金額のない施策の楽観", other["optimistic"].(map[string]any)["revenue"], "0")

	// 入力検証
	nextYear := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "2027予算", "fiscal_year": 2027})
	for name, q := range map[string]string{
		"基準なし":  "",
		"年度違い":  fmt.Sprintf("scenario_id=%d&optimistic_id=%d", f.budget, nextYear),
		"存在しない": "scenario_id=99999",
	} {
		if status, _ := f.viewer.do("GET", "/api/reports/risk?"+q, nil); status != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", name, status)
		}
	}
}
