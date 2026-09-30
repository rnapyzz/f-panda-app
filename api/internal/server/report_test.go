package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// reportValue は comparison のレスポンスから、指定した行・系列の金額を取り出す。
func reportValue(body map[string]any, functionID, activityID, subjectID int64, month, series string) string {
	for _, r := range body["rows"].([]any) {
		row := r.(map[string]any)
		if int64(row["function_id"].(float64)) != functionID || int64(row["subject_id"].(float64)) != subjectID || row["month"] != month {
			continue
		}
		if activityID != 0 {
			a, ok := row["activity_id"].(float64)
			if !ok || int64(a) != activityID {
				continue
			}
		}
		v, _ := row["values"].(map[string]any)[series].(string)
		return v
	}
	return ""
}

func TestComparisonReport(t *testing.T) {
	f := newScenarioFixture(t)
	amounts := func(sid, aid int64, items ...map[string]any) {
		t.Helper()
		if status, body := f.member.do("PUT", f.valuesPath(sid, aid)+"/amounts", map[string]any{"reason": "r", "amounts": items}); status != http.StatusOK {
			t.Fatalf("amounts: status = %d, body = %v", status, body)
		}
	}
	amt := func(subject int64, month string, amount int) map[string]any {
		return map[string]any{"subject_id": subject, "target_month": month, "amount": amount}
	}

	// 予算: 4月・10月に受託売上、見込: 10月だけ増額、実績: 4月
	amounts(f.budget, f.manualAct, amt(f.sales, "2026-04", 1000), amt(f.sales, "2026-10", 2000), amt(f.cost, "2026-04", 300))
	forecast := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "10月見込", "scenario_kind": "forecast", "fiscal_year": 2026, "base_scenario_id": f.budget})
	amounts(forecast, f.manualAct, amt(f.sales, "2026-10", 2500))
	// 同じ機能の別施策にも金額を入れ、機能単位では合算されることを確認する
	amounts(f.budget, f.formulaAct, amt(f.cost, "2026-04", 50))

	actual := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "実績", "scenario_kind": "actual", "fiscal_year": 2026})
	csv := "target_month,activity_code,subject_code,amount\n2026-04,PRJ-1,4110,900\n2026-10,PRJ-1,4110,9999\n"
	if status, body := f.admin.upload(fmt.Sprintf("/api/scenarios/%d/actuals/import", actual), csv, "実績取込"); status != http.StatusOK {
		t.Fatalf("実績取込: status = %d, body = %v", status, body)
	}

	path := fmt.Sprintf("/api/reports/comparison?scenario_ids=%d,%d&landing_actual_id=%d&landing_forecast_id=%d&landing_through=2026-09", f.budget, forecast, actual, forecast)
	body := f.viewer.mustGet(path)

	series := body["series"].([]any)
	if len(series) != 3 || series[2].(map[string]any)["key"] != "landing" {
		t.Fatalf("series = %v", series)
	}
	tests := []struct {
		name          string
		subject       int64
		month, series string
		want          string
	}{
		{"予算 4月 売上", f.sales, "2026-04", "s1", "1000"},
		{"見込 10月 売上", f.sales, "2026-10", "s2", "2500"},
		{"機能単位で2施策の費用を合算", f.cost, "2026-04", "s1", "350"},
		{"着地見込: 9月までは実績", f.sales, "2026-04", "landing", "900"},
		{"着地見込: 10月以降は見込（実績の10月は使わない）", f.sales, "2026-10", "landing", "2500"},
		{"着地見込: 実績にない費用は含まない", f.cost, "2026-04", "landing", ""},
	}
	for _, tt := range tests {
		if got := reportValue(body, f.fn1, 0, tt.subject, tt.month, tt.series); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}

	// 機能を指定すると施策ごとに集計する
	body = f.viewer.mustGet(fmt.Sprintf("/api/reports/comparison?scenario_ids=%d&function_id=%d", f.budget, f.fn1))
	if got := reportValue(body, f.fn1, f.manualAct, f.cost, "2026-04", "s1"); got != "300" {
		t.Errorf("施策別の費用 = %q, want 300", got)
	}
	if got := reportValue(body, f.fn1, f.formulaAct, f.cost, "2026-04", "s1"); got != "50" {
		t.Errorf("施策別の費用（別施策）= %q, want 50", got)
	}
}

func TestComparisonReportValidation(t *testing.T) {
	f := newScenarioFixture(t)
	other := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "2027予算", "scenario_kind": "budget", "fiscal_year": 2027})
	tests := []struct {
		name, query, field string
	}{
		{"指定なし", "", "scenario_ids"},
		{"年度違い", fmt.Sprintf("scenario_ids=%d,%d", f.budget, other), "scenario_ids"},
		{"存在しない", "scenario_ids=99999", "scenario_ids"},
		{"5つ以上", fmt.Sprintf("scenario_ids=%d,1,2,3,4", f.budget), "scenario_ids"},
		{"着地見込の指定不足", fmt.Sprintf("scenario_ids=%d&landing_actual_id=%d", f.budget, f.budget), "landing"},
		{"着地見込の実績が実績シナリオでない", fmt.Sprintf("landing_actual_id=%d&landing_forecast_id=%d&landing_through=2026-09", f.budget, f.budget), "landing_actual_id"},
	}
	for _, tt := range tests {
		status, body := f.viewer.do("GET", "/api/reports/comparison?"+tt.query, nil)
		if status != http.StatusUnprocessableEntity || detail(body, tt.field) == "" {
			t.Errorf("%s: status = %d, body = %v", tt.name, status, body)
		}
	}
	actual := f.admin.mustCreate("/api/scenarios", map[string]any{"name": "実績", "scenario_kind": "actual", "fiscal_year": 2026})
	status, body := f.viewer.do("GET", fmt.Sprintf("/api/reports/comparison?landing_actual_id=%d&landing_forecast_id=%d&landing_through=2027-04", actual, f.budget), nil)
	if status != http.StatusUnprocessableEntity || detail(body, "landing_through") == "" {
		t.Errorf("年度外の月: status = %d, body = %v", status, body)
	}
}
