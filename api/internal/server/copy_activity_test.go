package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

// TestCopyActivity は、既存の施策をコピーして作る（docs/plan.md「2.27」）ことを確かめる。
func TestCopyActivity(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	// コピー元: 計算式の内訳とドライバー（fixture）に、閲覧制限のある科目の内訳と金額を足す
	personnel := a.mustCreate("/api/subjects", map[string]any{"code": "8210", "name": "人件費", "category": "expense", "is_restricted": true})
	a.mustCreate(fmt.Sprintf("/api/activities/%d/lines", f.formulaAct), map[string]any{"subject_id": personnel, "name": "担当者の人件費", "reason": "r"})
	if status, body := a.do("PUT", f.valuesPath(f.budget, f.formulaAct)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.cost, "target_month": "2026-10", "amount": 100}}}); status != http.StatusOK {
		t.Fatalf("金額: %d %v", status, body)
	}

	// マネージャー（所管ユニット）がコピーして作る: ドライバーと内訳は写すが、閲覧制限のある科目の内訳は写さない
	status, body := f.manager1.do("POST", "/api/activities", activityBody(f.fn1, "", map[string]any{"name": "新しい案件", "copy_from_activity_id": f.formulaAct}))
	if status != http.StatusCreated || body["skipped_restricted_lines"] != 1.0 {
		t.Fatalf("コピー: %d %v", status, body)
	}
	copied := a.mustGet(fmt.Sprintf("/api/activities/%v", body["id"]))
	if n := len(copied["drivers"].([]any)); n != 2 {
		t.Errorf("ドライバー = %d 件, want 2", n)
	}
	lines := copied["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["expression"] != "unit_price * volume * 0.5" || lines[0].(map[string]any)["formula_enabled"] != true {
		t.Errorf("内訳 = %v, want 計算式の内訳だけ", lines)
	}
	// 金額は写さない
	if got, _ := amountOf(a.mustGet(f.valuesPath(f.budget, int64(body["id"].(float64)))), f.cost, "2026-10"); got != "" {
		t.Errorf("金額が写った: %q", got)
	}

	// FP&A は閲覧制限のある科目の内訳も写せる
	status, body = a.do("POST", "/api/activities", activityBody(f.fn1, "", map[string]any{"name": "FP&A のコピー", "copy_from_activity_id": f.formulaAct}))
	if status != http.StatusCreated || body["skipped_restricted_lines"] != 0.0 {
		t.Fatalf("FP&A のコピー: %d %v", status, body)
	}
	if n := len(a.mustGet(fmt.Sprintf("/api/activities/%v", body["id"]))["lines"].([]any)); n != 2 {
		t.Errorf("FP&A のコピーの内訳 = %d 件, want 2", n)
	}

	// コピー元がなければ 422、権限のないユニットには作れない
	if status, body := a.do("POST", "/api/activities", activityBody(f.fn1, "", map[string]any{"copy_from_activity_id": 999999})); status != http.StatusUnprocessableEntity || detail(body, "copy_from_activity_id") == "" {
		t.Errorf("コピー元なし: %d %v", status, body)
	}
	if status, _ := f.manager1.do("POST", "/api/activities", activityBody(f.fn2, "", map[string]any{"copy_from_activity_id": f.formulaAct})); status != http.StatusForbidden {
		t.Errorf("所管外のユニット: status = %d, want 403", status)
	}
}
