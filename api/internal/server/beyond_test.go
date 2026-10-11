package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestBeyondMonths は、年度をまたぐ施策の翌年度の月（docs/plan.md「2.26」）を確かめる。
func TestBeyondMonths(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	// 10月着手・翌年6月納品のプロジェクト（翌年度にかかる）と、年度内に終わる施策
	project := a.mustCreate("/api/activities", activityBody(f.fn1, "PJ-SPAN", map[string]any{
		"owner_user_id": f.memberID, "activity_type": "project", "start_date": "2026-10-01", "end_date": "2027-06-30",
	}))
	inYear := a.mustCreate("/api/activities", activityBody(f.fn1, "PJ-IN", map[string]any{
		"owner_user_id": f.memberID, "activity_type": "project", "start_date": "2026-10-01", "end_date": "2027-01-31",
	}))
	put := func(aid int64, month string, amount int) (int, map[string]any) {
		return f.member.do("PUT", f.valuesPath(f.budget, aid)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": month, "amount": amount}}})
	}

	// 当期の見込でも、翌年度にかかる施策は翌年度の月を入力できる
	if status, body := put(project, "2026-12", 1000); status != http.StatusOK {
		t.Fatalf("当期の月: %d %v", status, body)
	}
	status, body := put(project, "2027-06", 5000)
	if status != http.StatusOK {
		t.Fatalf("翌年度の月: %d %v", status, body)
	}
	if n := len(body["beyond_months"].([]any)); n != 12 || body["beyond_months"].([]any)[0] != "2027-04" {
		t.Errorf("翌年度の月 = %v", body["beyond_months"])
	}
	if got, _ := amountOf(body, f.sales, "2027-06"); got != "5000" {
		t.Errorf("翌年度の金額 = %q, want 5000", got)
	}
	// 年度内に終わる施策・翌々年度は入力できない
	if status, body := put(inYear, "2027-06", 1); status != http.StatusUnprocessableEntity {
		t.Errorf("年度内の施策の翌年度: %d %v", status, body)
	}
	if v := a.mustGet(f.valuesPath(f.budget, inYear)); len(v["beyond_months"].([]any)) != 0 {
		t.Errorf("年度内の施策の翌年度の月 = %v", v["beyond_months"])
	}
	if status, _ := put(project, "2028-04", 1); status != http.StatusUnprocessableEntity {
		t.Errorf("翌々年度: status = %d, want 422", status)
	}

	// 計算式: 翌年度の月のドライバー値からも金額を計算する
	if status, body := f.member.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/driver-values", map[string]any{"reason": "r", "values": []map[string]any{
		{"driver_id": f.priceID, "target_month": "2027-05", "value": 1000},
		{"driver_id": f.volumeID, "target_month": "2027-05", "value": 4},
	}}); status != http.StatusOK {
		t.Fatalf("翌年度のドライバー値: %d %v", status, body)
	} else if got, _ := lineAmountOf(body, f.salesLine, "2027-05"); got != "2000" {
		t.Errorf("翌年度の計算式の金額 = %q, want 2000", got)
	}

	// 年度の集計には翌年度の月を含めない（ホーム・予実比較）
	for _, it := range a.mustGet(fmt.Sprintf("/api/scenarios/%d/activity-status?scope=all", f.budget))["items"].([]any) {
		if m := it.(map[string]any); m["code"] == "PJ-SPAN" && m["current"].(map[string]any)["revenue"] != "1000" {
			t.Errorf("ホームの年間の売上 = %v, want 1000（翌年度を含めない）", m["current"])
		}
	}
	report := a.mustGet(fmt.Sprintf("/api/reports/comparison?scenario_ids=%d&unit_id=%d", f.budget, f.fn1))
	if got := reportValue(report, f.fn1, project, f.sales, "2027-06", "s1"); got != "" {
		t.Errorf("予実比較に翌年度の月がある: %q", got)
	}

	// 月次の見込を始めると翌年度の月も複製し、新年度の期初計画は翌年度の金額をそのまま引き継ぐ（プロジェクト型でも）
	next := a.mustCreate("/api/scenarios", map[string]any{"name": "11月見込", "fiscal_year": 2026, "base_scenario_id": f.budget})
	if got, _ := amountOf(a.mustGet(f.valuesPath(next, project)), f.sales, "2027-06"); got != "5000" {
		t.Errorf("複製した版の翌年度の金額 = %q", got)
	}
	status, body = a.do("POST", "/api/scenarios/start-fiscal-year", map[string]any{"fiscal_year": 2027, "name": "2027年度 期初計画", "source": "current"})
	if status != http.StatusCreated {
		t.Fatalf("新年度: %d %v", status, body)
	}
	plan := int64(body["scenario"].(map[string]any)["id"].(float64))
	planView := a.mustGet(f.valuesPath(plan, project))
	if got, _ := amountOf(planView, f.sales, "2027-06"); got != "5000" {
		t.Errorf("翌期の計画の金額 = %q, want 5000", got)
	}
	if got, _ := amountOf(planView, f.sales, "2027-07"); got != "" {
		t.Errorf("翌期の計画の 7月 = %q, want 空（3月の金額を広げない）", got)
	}
	if got, _ := lineAmountOf(a.mustGet(f.valuesPath(plan, f.formulaAct)), f.salesLine, "2027-05"); got != "2000" {
		t.Errorf("翌期の計画の計算式の金額 = %q, want 2000", got)
	}
}
