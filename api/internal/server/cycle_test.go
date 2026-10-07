package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestStartMonthly は docs/plan.md「2.16」の「月次の見込を始める」を確かめる。
func TestStartMonthly(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	// 作成中の版（期初計画）に 9月の計画値を入れ、4月の実績を取り込む
	if status, body := a.do("PUT", f.valuesPath(f.budget, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-09", "amount": 700}}}); status != http.StatusOK {
		t.Fatalf("amounts: %d %v", status, body)
	}
	a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,PRJ-1,4110,900\n", "4月実績")

	req := map[string]any{"name": "2026年5月見込", "actual_through": "2026-05", "update_deadline": "2026-06-10"}
	// dry run: 変わる内容と注意（5月の実績が未取込、未完了の施策）を返し、何も変えない
	dry := map[string]any{"dry_run": true}
	for k, v := range req {
		dry[k] = v
	}
	status, body := a.do("POST", "/api/scenarios/start-monthly", dry)
	if status != http.StatusOK || body["lock"].(map[string]any)["name"] != "2026年度 当初予算" {
		t.Fatalf("dry run: status = %d, body = %v", status, body)
	}
	warnings := fmt.Sprint(body["warnings"])
	if !strings.Contains(warnings, "2026-05") || !strings.Contains(warnings, "完了にしていない施策が 2 件") {
		t.Errorf("注意 = %v", warnings)
	}
	if items := a.mustGet("/api/scenarios")["items"].([]any); len(items) != 1 {
		t.Fatalf("dry run なのにシナリオが増えた: %d", len(items))
	}

	// 実行: 前の版をロック → 複製 → 決算確定月・最新見込・前回見込・作成中・締切
	status, body = a.do("POST", "/api/scenarios/start-monthly", req)
	if status != http.StatusCreated {
		t.Fatalf("実行: status = %d, body = %v", status, body)
	}
	s := body["scenario"].(map[string]any)
	if s["actual_through"] != "2026-05" || s["plan_role"] != "latest" || s["is_active"] != true || s["update_deadline"] != "2026-06-10" ||
		int64(s["previous_scenario_id"].(float64)) != f.budget {
		t.Errorf("新しい版 = %v", s)
	}
	newID := int64(s["id"].(float64))
	old := a.mustGet(fmt.Sprintf("/api/scenarios/%d", f.budget))
	if old["is_locked"] != true || old["is_active"] != false || old["plan_role"] != "initial" {
		t.Errorf("前の版 = %v", old)
	}
	// 計画値が複製され、4月は実績になっている
	view := a.mustGet(f.valuesPath(newID, f.manualAct))
	if got, _ := amountOf(view, f.sales, "2026-09"); got != "700" {
		t.Errorf("複製した9月 = %q", got)
	}
	if got, _ := amountOf(view, f.sales, "2026-04"); got != "900" {
		t.Errorf("4月の実績 = %q", got)
	}
	// 担当者に「更新の開始」が届く
	if items := f.member.mustGet("/api/notifications")["items"].([]any); len(items) != 1 {
		t.Errorf("更新の開始のお知らせ = %v", items)
	}

	// 翌月: 決算確定月は前の版以降。最新見込は付け替わる
	if status, body := a.do("POST", "/api/scenarios/start-monthly", map[string]any{"name": "戻す", "actual_through": "2026-04"}); status != http.StatusUnprocessableEntity || detail(body, "actual_through") == "" {
		t.Errorf("前より前の決算確定月: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/scenarios/start-monthly", map[string]any{"name": "別年度", "actual_through": "2027-04"}); status != http.StatusUnprocessableEntity || !strings.Contains(detail(body, "actual_through"), "新年度") {
		t.Errorf("別の年度: status = %d, body = %v", status, body)
	}
	status, body = a.do("POST", "/api/scenarios/start-monthly", map[string]any{"name": "2026年6月見込", "actual_through": "2026-06"})
	if status != http.StatusCreated || body["role_from"].(map[string]any)["name"] != "2026年5月見込" {
		t.Fatalf("翌月: status = %d, body = %v", status, body)
	}
	if prev := a.mustGet(fmt.Sprintf("/api/scenarios/%d", newID)); prev["plan_role"] != nil || prev["is_locked"] != true {
		t.Errorf("翌月の後の前の版 = %v", prev)
	}
	if status, _ := f.manager1.do("POST", "/api/scenarios/start-monthly", req); status != http.StatusForbidden {
		t.Errorf("マネージャー: status = %d", status)
	}
}

func TestStartFiscalYear(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	status, body := a.do("POST", "/api/scenarios/start-fiscal-year", map[string]any{"fiscal_year": 2027, "name": "2027年度 期初計画", "dry_run": true})
	if status != http.StatusOK || !strings.Contains(fmt.Sprint(body["warnings"]), "2026年度）を締めていません") {
		t.Fatalf("dry run: status = %d, body = %v", status, body)
	}
	status, body = a.do("POST", "/api/scenarios/start-fiscal-year", map[string]any{"fiscal_year": 2027, "name": "2027年度 期初計画"})
	s, _ := body["scenario"].(map[string]any)
	if status != http.StatusCreated || s["fiscal_year"] != float64(2027) || s["plan_role"] != "initial" || s["actual_through"] != nil || s["is_active"] != true {
		t.Fatalf("新年度: status = %d, body = %v", status, body)
	}
	if old := a.mustGet(fmt.Sprintf("/api/scenarios/%d", f.budget)); old["is_locked"] != true {
		t.Errorf("前の作成中の版がロックされていない: %v", old)
	}
	// 新しい版は空
	if got, _ := amountOf(a.mustGet(f.valuesPath(int64(s["id"].(float64)), f.manualAct)), f.sales, "2027-04"); got != "" {
		t.Errorf("新年度の版に金額がある: %q", got)
	}
}
