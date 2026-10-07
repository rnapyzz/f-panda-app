package server_test

import (
	"fmt"
	"net/http"
	"strings"
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

func plOf(a map[string]any, key string) string {
	p := a[key].(map[string]any)
	return fmt.Sprintf("%v/%v", p["revenue"], p["expense"])
}

// TestRiskMeasures は docs/plan.md「2.8」の例（楽観・基準・悲観）を確かめる。
func TestRiskMeasures(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	line := func(act int64, name, level, outlook string) int64 {
		return a.mustCreate(fmt.Sprintf("/api/activities/%d/lines", act), map[string]any{"subject_id": f.sales, "name": name, "confidence_level": level, "outlook": outlook, "reason": "r"})
	}
	put := func(act int64, items ...map[string]any) {
		t.Helper()
		if status, body := a.do("PUT", f.valuesPath(f.budget, act)+"/amounts", map[string]any{"reason": "r", "amounts": items}); status != http.StatusOK {
			t.Fatalf("amounts: status = %d, body = %v", status, body)
		}
	}
	item := func(lineID int64, amount int) map[string]any {
		return map[string]any{"subject_id": f.sales, "line_id": lineID, "target_month": "2026-10", "amount": amount}
	}
	// フロー型（PRJ-1）: 案件 1,000（C・ベース）、追加要件 200（D・アドオン）
	put(f.manualAct, item(line(f.manualAct, "案件", "C", "base"), 1000), item(line(f.manualAct, "追加要件", "D", "addon"), 200))
	// ストック型: ベース 500（A）、アドオン 100（C）、ダウンサイド −50（D）
	stock := a.mustCreate("/api/activities", activityBody(f.fn1, "STOCK-1", map[string]any{"confidence_level": "A"}))
	put(stock, item(line(stock, "既存顧客", "A", "base"), 500), item(line(stock, "新規", "C", "addon"), 100), item(line(stock, "解約", "D", "downside"), -50))

	body := a.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d", f.budget))
	flow := riskActivity(body, "PRJ-1")
	if got := plOf(flow, "optimistic") + " " + plOf(flow, "weighted") + " " + plOf(flow, "pessimistic"); got != "1200/0 540/0 0/0" {
		t.Errorf("フロー型 楽観・基準・悲観 = %s, want 1200/0 540/0 0/0", got)
	}
	st := riskActivity(body, "STOCK-1")
	if got := plOf(st, "optimistic") + " " + plOf(st, "weighted") + " " + plOf(st, "pessimistic"); got != "600/0 540/0 450/0" {
		t.Errorf("ストック型 楽観・基準・悲観 = %s, want 600/0 540/0 450/0", got)
	}
	if got := fmt.Sprint(st["revenue_by_level"]); got != "map[A:500 C:100 downside:-50]" {
		t.Errorf("段階別の売上 = %s", got)
	}
	if lines := st["lines"].([]any); len(lines) != 3 || lines[2].(map[string]any)["outlook"] != "downside" || lines[2].(map[string]any)["amount"] != "-50" {
		t.Errorf("内訳 = %v", lines)
	}

	// 予実比較の measure
	cmp := a.mustGet(fmt.Sprintf("/api/reports/comparison?scenario_ids=%d&unit_id=%d&measure=weighted", f.budget, f.fn1))
	var weighted int
	for _, r := range cmp["rows"].([]any) {
		row := r.(map[string]any)
		var v int
		fmt.Sscan(row["values"].(map[string]any)["s1"].(string), &v)
		weighted += v
	}
	if weighted != 1080 || cmp["measure"] != "weighted" {
		t.Errorf("予実比較の加重見込の合計 = %d, want 1080", weighted)
	}
	if status, _ := a.do("GET", fmt.Sprintf("/api/reports/comparison?scenario_ids=%d&measure=x", f.budget), nil); status != http.StatusBadRequest {
		t.Errorf("不正な measure: status = %d", status)
	}

	// 実績の月はどの指標でも実績の金額。期間「残り」なら実績の月を除く
	a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,PRJ-1,4110,300\n", "4月実績")
	a.do("PUT", fmt.Sprintf("/api/scenarios/%d", f.budget), map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "actual_through": "2026-04", "reason": "4月確定"})
	body = a.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d", f.budget))
	flow = riskActivity(body, "PRJ-1")
	if plOf(flow, "pessimistic") != "300/0" || plOf(flow, "actual") != "300/0" || flow["revenue_by_level"].(map[string]any)["actual"] != "300" {
		t.Errorf("実績を含む悲観 = %s, actual = %s", plOf(flow, "pessimistic"), plOf(flow, "actual"))
	}
	body = a.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d&period=remaining", f.budget))
	if flow = riskActivity(body, "PRJ-1"); plOf(flow, "pessimistic") != "0/0" || len(body["months"].([]any)) != 11 {
		t.Errorf("残り期間の悲観 = %s, months = %v", plOf(flow, "pessimistic"), body["months"])
	}
}

// TestRiskWarnings は docs/plan.md「2.8」の客観的なシグナル（警告）を確かめる。
func TestRiskWarnings(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	put := func(sid int64, month string, amount int) {
		t.Helper()
		if status, body := a.do("PUT", f.valuesPath(sid, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": month, "amount": amount}}}); status != http.StatusOK {
			t.Fatalf("amounts: status = %d, body = %v", status, body)
		}
	}
	// PRJ-1 を確度 A にする（「段階に対して状況が悪い」の対象）
	act := a.mustGet(fmt.Sprintf("/api/activities/%d", f.manualAct))
	a.do("PUT", fmt.Sprintf("/api/activities/%d", f.manualAct), activityBody(f.fn1, "PRJ-1", map[string]any{"owner_user_id": f.memberID, "confidence_level": "A", "name": act["name"], "reason": "受注"}))

	// 見込の推移: 期初 1,000,000 → 9月見込 800,000 → 10月見込 500,000（2回続けて下がる）。4月の計画 400,000 に対し実績 200,000
	put(f.budget, "2026-04", 400_000)
	put(f.budget, "2026-10", 600_000)
	sep := a.mustCreate("/api/scenarios", map[string]any{"name": "9月見込", "fiscal_year": 2026, "base_scenario_id": f.budget})
	put(sep, "2026-10", 400_000)
	ms := a.mustCreate(fmt.Sprintf("/api/activities/%d/milestones", f.manualAct), map[string]any{"name": "検収", "due_date": time.Now().AddDate(0, 0, 10).Format("2006-01-02")})
	oct := a.mustCreate("/api/scenarios", map[string]any{"name": "10月見込", "fiscal_year": 2026, "base_scenario_id": sep})
	put(oct, "2026-10", 100_000)
	a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,PRJ-1,4110,200000\n", "4月実績")
	a.do("PUT", fmt.Sprintf("/api/scenarios/%d", oct), map[string]any{"name": "10月見込", "actual_through": "2026-04", "reason": "4月確定"})
	// 比較シナリオの作成後に、マイルストーンの期日を後ろにずらす
	a.do("PUT", fmt.Sprintf("/api/activities/%d/milestones/%d", f.manualAct, ms), map[string]any{"name": "検収", "due_date": time.Now().AddDate(0, 0, 40).Format("2006-01-02"), "status": "in_progress", "reason": "先方都合"})

	body := a.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d&compare_id=%d", oct, sep))
	p := riskActivity(body, "PRJ-1")
	w := p["warnings"].(map[string]any)
	if d := w["downward"]; d == nil || d.(map[string]any)["diff"] != "-500000" {
		t.Errorf("下方修正 = %v", d)
	}
	if w["consecutive"] != true {
		t.Errorf("連続の下方修正 = %v", w["consecutive"])
	}
	if acc := w["accuracy"]; acc == nil || acc.(float64) != 50 {
		t.Errorf("当たり具合 = %v, want 50", acc)
	}
	if pp := w["postponed"].(map[string]any); pp["count"] != float64(1) || pp["days"] != float64(30) {
		t.Errorf("後ろ倒し = %v", pp)
	}
	if p["warning_count"] != float64(4) || p["bad_for_level"] != true {
		t.Errorf("警告の数 = %v, 段階に対して状況が悪い = %v", p["warning_count"], p["bad_for_level"])
	}
	if c := p["compare"].(map[string]any); c["revenue"] != "800000" {
		t.Errorf("比較シナリオの加重見込 = %v", c)
	}
	// 比較シナリオなしでは、比較に基づく警告は出ない
	body = a.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d", oct))
	if p = riskActivity(body, "PRJ-1"); p["compare"] != nil || p["warnings"].(map[string]any)["downward"] != nil {
		t.Errorf("比較なし = %v", p)
	}
	if status, body := a.do("GET", fmt.Sprintf("/api/reports/risk?scenario_id=%d&period=x", oct), nil); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body), "period") {
		t.Errorf("不正な period: status = %d", status)
	}
}
