package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestScenarioNarrative は docs/plan.md「2.31」のシナリオの説明を確かめる。
func TestScenarioNarrative(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	path := fmt.Sprintf("/api/scenarios/%d/narrative", f.budget)

	// 書いていなければ空の欄
	if body := f.viewer.mustGet(path); body["outline"] != "" || body["risks"] != "" || body["updated_at"] != nil {
		t.Errorf("書く前 = %v", body)
	}
	in := map[string]any{"outline": " 既存の深耕で増収増益 ", "growth": "SaaS の単価改定", "premises": "解約率 1%", "risks": "大型案件の後ろ倒し"}
	status, body := a.do("PUT", path, in)
	if status != http.StatusOK || body["outline"] != "既存の深耕で増収増益" || body["risks"] != "大型案件の後ろ倒し" || body["updated_by_name"] == "" {
		t.Fatalf("保存: %d %v", status, body)
	}
	// だれでも読める
	if body := f.member.mustGet(path); body["growth"] != "SaaS の単価改定" {
		t.Errorf("メンバーが読む = %v", body)
	}
	// 書けるのは FP&A だけ
	for name, c := range map[string]*client{"マネージャー": f.manager1, "メンバー": f.member} {
		if status, _ := c.do("PUT", path, in); status != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, status)
		}
	}
	// 文字数の上限
	if status, body := a.do("PUT", path, map[string]any{"risks": strings.Repeat("あ", 2001)}); status != http.StatusUnprocessableEntity || detail(body, "risks") == "" {
		t.Errorf("長すぎる: %d %v", status, body)
	}
	// 変更理由は任意で、空ならシナリオ名から作る。変更前後は監査ログに残る
	in["premises"] = "解約率 2%"
	if status, body := a.do("PUT", path, in); status != http.StatusOK {
		t.Fatalf("更新: %d %v", status, body)
	}
	sets := a.mustGet(fmt.Sprintf("/api/change-sets?scenario_id=%d&limit=2", f.budget))
	if got := changeSetReasons(sets); got[0] != "2026年度 当初予算の説明を更新" {
		t.Errorf("理由 = %q", got)
	}
	if tables := sets["items"].([]any)[0].(map[string]any)["tables"].(map[string]any); tables["scenario_narratives"] != float64(1) {
		t.Errorf("監査ログ = %v", tables)
	}

	// 月次の見込を始めると引き継ぐ。前の版はロックされて直せない
	status, body = a.do("POST", "/api/scenarios/start-monthly", map[string]any{"name": "2026年5月見込", "actual_through": "2026-05"})
	if status != http.StatusCreated {
		t.Fatalf("月次の見込: %d %v", status, body)
	}
	next := int64(body["scenario"].(map[string]any)["id"].(float64))
	if got := a.mustGet(fmt.Sprintf("/api/scenarios/%d/narrative", next)); got["premises"] != "解約率 2%" || got["outline"] != "既存の深耕で増収増益" {
		t.Errorf("引き継いだ説明 = %v", got)
	}
	if status, _ := a.do("PUT", path, in); status != http.StatusConflict {
		t.Errorf("ロック済み: status = %d, want 409", status)
	}

	// 新年度の期初計画は空で始める
	status, body = a.do("POST", "/api/scenarios/start-fiscal-year", map[string]any{"fiscal_year": 2027, "name": "2027年度 期初計画", "source": "current"})
	if status != http.StatusCreated {
		t.Fatalf("新年度: %d %v", status, body)
	}
	plan := int64(body["scenario"].(map[string]any)["id"].(float64))
	if got := a.mustGet(fmt.Sprintf("/api/scenarios/%d/narrative", plan)); got["outline"] != "" {
		t.Errorf("新年度の説明 = %v", got)
	}
	if status, _ := a.do("GET", "/api/scenarios/999999/narrative", nil); status != http.StatusNotFound {
		t.Errorf("ないシナリオ: status = %d", status)
	}
}
