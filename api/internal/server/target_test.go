package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestUpdateTargets は docs/plan.md「2.10」の更新の対象を確かめる（I-18）。
// 完了・中止の施策は、計画値の月に金額が残っていなければ、ホーム・通知・切り替えの注意の対象にしない。
func TestUpdateTargets(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	setStatus := func(id int64, code, status string) {
		t.Helper()
		act := a.mustGet(fmt.Sprintf("/api/activities/%d", id))
		body := activityBody(f.fn1, code, map[string]any{"owner_user_id": f.memberID, "status": status, "name": act["name"], "confidence_level": act["confidence_level"], "reason": "r"})
		if status, res := a.do("PUT", fmt.Sprintf("/api/activities/%d", id), body); status != http.StatusOK {
			t.Fatalf("status: %d %v", status, res)
		}
	}
	codes := func() []string {
		var out []string
		for _, it := range a.mustGet(fmt.Sprintf("/api/scenarios/%d/activity-status?scope=all", f.budget))["items"].([]any) {
			out = append(out, it.(map[string]any)["code"].(string))
		}
		return out
	}
	// PRJ-1 は中止で金額なし（対象外）、SAAS-1 は完了だが 10月に金額が残っている（対象）、DONE-1 は完了で金額なし（対象外）
	setStatus(f.manualAct, "PRJ-1", "cancelled")
	setStatus(f.formulaAct, "SAAS-1", "completed")
	if status, body := a.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.cost, "target_month": "2026-10", "amount": 100}}}); status != http.StatusOK {
		t.Fatalf("amounts: %d %v", status, body)
	}
	a.mustCreate("/api/activities", activityBody(f.fn1, "DONE-1", map[string]any{"owner_user_id": f.memberID, "status": "completed"}))
	a.mustCreate("/api/activities", activityBody(f.fn1, "NEW-1", map[string]any{"owner_user_id": f.memberID, "status": "planned"}))

	if got := strings.Join(codes(), ","); got != "NEW-1,SAAS-1" {
		t.Errorf("ホームの対象 = %s, want NEW-1,SAAS-1", got)
	}

	// 締切の通知・切り替えの注意も同じ対象（未完了 2 件）
	a.do("PUT", fmt.Sprintf("/api/scenarios/%d", f.budget), map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "update_deadline": "2026-10-15"})
	items := f.member.mustGet("/api/notifications")["items"].([]any)
	if len(items) != 1 || !strings.Contains(items[0].(map[string]any)["body"].(string), "（2件）") || strings.Contains(items[0].(map[string]any)["body"].(string), "PRJ-1") {
		t.Errorf("更新の開始のお知らせ = %v", items)
	}
	_, body := a.do("POST", "/api/scenarios/start-monthly", map[string]any{"name": "5月見込", "actual_through": "2026-05", "dry_run": true})
	if !strings.Contains(fmt.Sprint(body["warnings"]), "完了にしていない施策が 2 件") {
		t.Errorf("切り替えの注意 = %v", body["warnings"])
	}
}
