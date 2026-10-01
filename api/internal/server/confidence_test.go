package server_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestConfidenceLevels(t *testing.T) {
	f := newActivityFixture(t)

	// 初期値は A〜E
	items := f.viewer.mustGet("/api/confidence-levels")["items"].([]any)
	if len(items) != 5 {
		t.Fatalf("初期の段階 = %d件, want 5", len(items))
	}
	if c := items[1].(map[string]any); c["code"] != "B" || numString(c["rate"]) != "0.8" || c["criteria"] == "" {
		t.Errorf("B = %v", c)
	}

	// 作成・更新は FP&A のみ
	if status, _ := f.manager1.do("POST", "/api/confidence-levels", map[string]any{"code": "F", "name": "x", "rate": 0.1}); status != http.StatusForbidden {
		t.Errorf("マネージャーの作成: status = %d, want 403", status)
	}
	for name, tc := range map[string]struct {
		body  map[string]any
		field string
	}{
		"コード形式":  {map[string]any{"code": "a b", "name": "x", "rate": 0.1}, "code"},
		"コード重複":  {map[string]any{"code": "A", "name": "x", "rate": 0.1}, "code"},
		"確率が範囲外": {map[string]any{"code": "F", "name": "x", "rate": 1.2}, "rate"},
		"確率の桁数":  {map[string]any{"code": "F", "name": "x", "rate": 0.12345}, "rate"},
		"名前なし":   {map[string]any{"code": "F", "rate": 0.1}, "name"},
	} {
		if status, body := f.admin.do("POST", "/api/confidence-levels", tc.body); status != http.StatusUnprocessableEntity || detail(body, tc.field) == "" {
			t.Errorf("%s: status = %d, body = %v", name, status, body)
		}
	}
	id := f.admin.mustCreate("/api/confidence-levels", map[string]any{"code": "F", "name": "撤退検討", "rate": "0.05", "criteria": "撤退の検討を始めた", "sort_order": 6})
	path := fmt.Sprintf("/api/confidence-levels/%d", id)
	status, body := f.admin.do("PUT", path, map[string]any{"code": "G", "name": "撤退検討", "rate": 0.1, "criteria": "撤退の検討を始めた", "sort_order": 6})
	if status != http.StatusOK || body["code"] != "F" || numString(body["rate"]) != "0.1" {
		t.Errorf("更新（コードは変わらない）: status = %d, body = %v", status, body)
	}

	// 施策に段階を付ける。省略時は施策タイプの既定（運用型は A）
	act := f.admin.mustCreate("/api/activities", activityBody(f.fn1, "ACT-1", nil))
	if got := f.viewer.mustGet(fmt.Sprintf("/api/activities/%d", act))["confidence_level"]; got != "A" {
		t.Errorf("既定の段階 = %v, want A", got)
	}
	f.admin.do("PUT", fmt.Sprintf("/api/activities/%d", act), activityBody(f.fn1, "ACT-1", map[string]any{"confidence_level": "F", "reason": "撤退の検討"}))

	// 施策から参照されている段階は削除できない
	if status, _ := f.admin.do("DELETE", path, nil); status != http.StatusConflict {
		t.Errorf("使用中の段階の削除: status = %d, want 409", status)
	}
	f.admin.do("PUT", fmt.Sprintf("/api/activities/%d", act), activityBody(f.fn1, "ACT-1", map[string]any{"confidence_level": "A", "reason": "継続"}))
	if status, _ := f.admin.do("DELETE", path, nil); status != http.StatusNoContent {
		t.Errorf("段階の削除: status = %d, want 204", status)
	}
}
