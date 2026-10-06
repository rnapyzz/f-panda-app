package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestActualCorrections は docs/plan.md「2.14 締めた後の実績の修正と年度の締め」を確かめる。
func TestActualCorrections(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	header := "target_month,box_code,account_code,amount\n"
	if status, body := a.upload(actualsPath, header+"2026-04,PRJ-1,4110,1000\n2026-05,PRJ-1,4110,500\n", "4・5月実績"); status != http.StatusOK {
		t.Fatalf("取込: status = %d, body = %v", status, body)
	}
	// 5月まで確定した版をロックする
	path := fmt.Sprintf("/api/scenarios/%d", f.budget)
	a.do("PUT", path, map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "actual_through": "2026-05", "reason": "5月確定"})
	if status, body := a.do("POST", path+"/lock", nil); status != http.StatusOK {
		t.Fatalf("ロック: status = %d, body = %v", status, body)
	}
	if items := a.mustGet("/api/scenarios/actual-drift")["items"].([]any); len(items) != 0 {
		t.Fatalf("ロック直後の食い違い = %v", items)
	}

	// 4月の実績を修正して取り込むと、確認の結果にロック済みのシナリオとの食い違いが出る（取り込みは止めない）
	status, body := a.upload(actualsPath+"?dry_run=true", header+"2026-04,PRJ-1,4110,1200\n2026-04,PRJ-1,8110,30\n", "")
	drift := body["locked_drift"].([]any)
	if status != http.StatusOK || len(drift) != 1 {
		t.Fatalf("確認の食い違い: status = %d, body = %v", status, body)
	}
	m := drift[0].(map[string]any)["months"].([]any)[0].(map[string]any)
	if m["month"] != "2026-04" || m["revenue"] != "200" || m["expense"] != "30" || m["changed"] != float64(2) {
		t.Errorf("4月の食い違い = %v", m)
	}
	a.upload(actualsPath, header+"2026-04,PRJ-1,4110,1200\n2026-04,PRJ-1,8110,30\n", "4月実績の修正")

	// ロック済みのシナリオは固定のまま。一覧に食い違いが出る
	vpath := f.valuesPath(f.budget, f.manualAct)
	if got, _ := amountOf(a.mustGet(vpath), f.sales, "2026-04"); got != "1000" {
		t.Errorf("ロック済みの4月 = %q, want 1000", got)
	}
	items := a.mustGet("/api/scenarios/actual-drift")["items"].([]any)
	if len(items) != 1 || int64(items[0].(map[string]any)["scenario_id"].(float64)) != f.budget {
		t.Fatalf("食い違いの一覧 = %v", items)
	}
	if status, _ := f.manager1.do("GET", "/api/scenarios/actual-drift", nil); status != http.StatusForbidden {
		t.Errorf("マネージャーの食い違いの一覧: status = %d", status)
	}

	// 実績を最新にする: dry run で差を確認し、理由付きで入れ替える。ロックは外れない
	refresh := path + "/refresh-actuals"
	if status, body := a.do("POST", refresh, map[string]any{"dry_run": true}); status != http.StatusOK || body["drift"].(map[string]any)["months"] == nil {
		t.Errorf("dry run: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", refresh, map[string]any{}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なし: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", refresh, map[string]any{"reason": "4月の決算整理を反映"}); status != http.StatusOK || body["saved"] != float64(3) {
		t.Fatalf("実績を最新にする: status = %d, body = %v", status, body)
	}
	view := a.mustGet(vpath)
	if got, _ := amountOf(view, f.sales, "2026-04"); got != "1200" || view["scenario"].(map[string]any)["is_locked"] != true {
		t.Errorf("最新にした後の4月 = %q, scenario = %v", got, view["scenario"])
	}
	if items := a.mustGet("/api/scenarios/actual-drift")["items"].([]any); len(items) != 0 {
		t.Errorf("最新にした後の食い違い = %v", items)
	}
	if status, _ := a.do("POST", refresh, map[string]any{"reason": "再"}); status != http.StatusConflict {
		t.Errorf("食い違いがないのに最新にした: status = %d", status)
	}

	// 年度を締めると、その年度の月は取り込めず、再割当もできない
	if status, body := a.do("POST", "/api/fiscal-years/2026/close", map[string]any{"reason": "2026年度決算確定"}); status != http.StatusOK || body["fiscal_year"] != float64(2026) {
		t.Fatalf("締め: status = %d, body = %v", status, body)
	}
	if status, _ := a.do("POST", "/api/fiscal-years/2026/close", nil); status != http.StatusConflict {
		t.Errorf("二重の締め: status = %d", status)
	}
	status, body = a.upload(actualsPath, header+"2026-04,PRJ-1,4110,9\n2026-04,PRJ-1,4110,9\n2027-04,PRJ-1,4110,9\n", "締めた年度")
	rows := body["error"].(map[string]any)["rows"].([]any)
	if status != http.StatusUnprocessableEntity || len(rows) != 1 || !strings.Contains(rows[0].(map[string]any)["message"].(string), "2026年度") {
		t.Errorf("締めた年度の取込: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/actuals/reallocate", map[string]any{"months": []string{"2026-04"}, "reason": "x"}); status != http.StatusUnprocessableEntity || detail(body, "months") == "" {
		t.Errorf("締めた年度の再割当: status = %d, body = %v", status, body)
	}
	if items := f.viewer.mustGet("/api/fiscal-years/closings")["items"].([]any); len(items) != 1 {
		t.Errorf("締めた年度の一覧 = %v", items)
	}

	// 締めの解除は理由が必須。解除すれば取り込める
	if status, body := a.do("POST", "/api/fiscal-years/2026/reopen", map[string]any{}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの解除: status = %d, body = %v", status, body)
	}
	if status, _ := a.do("POST", "/api/fiscal-years/2026/reopen", map[string]any{"reason": "修正の取込"}); status != http.StatusNoContent {
		t.Errorf("解除: status = %d", status)
	}
	if status, body := a.upload(actualsPath, header+"2026-04,PRJ-1,4110,1300\n", "締め解除後"); status != http.StatusOK {
		t.Errorf("解除後の取込: status = %d, body = %v", status, body)
	}
}

// TestAssignSkipsClosedYear は、未割当の割当で、締めた年度の行は割り当てずにルールだけを登録することを確かめる。
func TestAssignSkipsClosedYear(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,P-1,4110,100\n2027-04,P-1,4110,50\n", "取込")
	a.do("POST", "/api/fiscal-years/2026/close", nil)
	status, body := a.do("POST", "/api/actuals/unallocated/assign", map[string]any{"box_code": "P-1", "activity_id": f.manualAct, "reason": "新規案件"})
	if status != http.StatusOK || body["assigned"] != float64(1) || body["skipped_closed"] != float64(1) || body["external_code"] != "P-1" {
		t.Fatalf("割当: status = %d, body = %v", status, body)
	}
	if got := f.actualAmount(0, f.sales, "2026-04"); got != "100" {
		t.Errorf("締めた年度の未割当 = %q, want 100（そのまま）", got)
	}
	if got := f.actualAmount(f.manualAct, f.sales, "2027-04"); got != "50" {
		t.Errorf("締めていない年度の割当 = %q", got)
	}
}
