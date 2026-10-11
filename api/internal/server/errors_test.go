package server_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestErrorStatuses は、誤ったリクエストに正しいステータスコードを返すことを確かめる（500 にしない）。
func TestErrorStatuses(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	reason := map[string]any{"reason": "r"}

	// 存在しないシナリオへの更新は 404（変更セットの外部キーのエラーを 500 にしない）
	for _, tt := range []struct {
		method, path string
		body         map[string]any
	}{
		{"POST", "/api/scenarios/999999/activate", reason},
		{"POST", "/api/scenarios/999999/lock", reason},
		{"POST", "/api/scenarios/999999/unlock", reason},
		{"PUT", "/api/scenarios/999999", map[string]any{"name": "x", "reason": "r"}},
		{"POST", "/api/scenarios/999999/refresh-actuals", reason},
		{"PUT", fmt.Sprintf("/api/scenarios/999999/activities/%d/note", f.manualAct), map[string]any{"explanation": "x"}},
		{"PUT", fmt.Sprintf("/api/scenarios/999999/activities/%d/amounts", f.manualAct), map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-04", "amount": 1}}}},
	} {
		if status, body := a.do(tt.method, tt.path, tt.body); status != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, body = %v, want 404", tt.method, tt.path, status, body)
		}
	}

	// 実績・割当ルールのある施策の削除は 409
	withActual := a.mustCreate("/api/activities", activityBody(f.fn1, "ERR-ACT", nil))
	withRule := a.mustCreate("/api/activities", activityBody(f.fn1, "ERR-RULE", nil))
	if status, body := a.upload(actualsPath, "target_month,box_code,account_code,amount\n2026-04,ERR-ACT,4110,100\n", "実績"); status != http.StatusOK {
		t.Fatalf("実績取込: %d %v", status, body)
	}
	a.mustCreate("/api/allocation-rules", map[string]any{"gl_account_id": f.costAccount, "department_code": "D900", "activity_id": withRule, "reason": "ルール"})
	for name, id := range map[string]int64{"実績あり": withActual, "割当ルールあり": withRule} {
		if status, body := a.do("DELETE", fmt.Sprintf("/api/activities/%d", id), reason); status != http.StatusConflict {
			t.Errorf("%sの施策の削除: status = %d, body = %v, want 409", name, status, body)
		}
	}

	// 存在しないユニットの廃止・統合は 404、統合先がなければ 422
	if status, _ := a.do("POST", "/api/units/999999/archive", reason); status != http.StatusNotFound {
		t.Errorf("存在しないユニットの廃止: status = %d, want 404", status)
	}
	if status, _ := a.do("POST", "/api/units/999999/merge", map[string]any{"target_unit_id": f.fn1, "reason": "r"}); status != http.StatusNotFound {
		t.Errorf("存在しないユニットの統合: status = %d, want 404", status)
	}
	if status, body := a.do("POST", fmt.Sprintf("/api/units/%d/merge", f.fn2), map[string]any{"target_unit_id": 999999, "reason": "r"}); status != http.StatusUnprocessableEntity || detail(body, "target_unit_id") == "" {
		t.Errorf("存在しない統合先: status = %d, body = %v, want 422", status, body)
	}

	// メソッド違いは 405（Allow 付き）、存在しない API は 404
	req, _ := http.NewRequest("PATCH", a.base+"/api/activities", nil)
	res, err := a.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") != "GET, POST" || !strings.Contains(string(b), "method_not_allowed") {
		t.Errorf("PATCH /api/activities: status = %d, Allow = %q, body = %s", res.StatusCode, res.Header.Get("Allow"), b)
	}
	if status, _ := a.do("GET", "/api/no-such-api", nil); status != http.StatusNotFound {
		t.Errorf("存在しない API: status = %d, want 404", status)
	}

	// JSON の誤りは 400。内部の型名は出さない
	if status, body := a.do("POST", "/api/activities", map[string]any{"name": 123}); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body), "項目 name の形式が正しくありません") {
		t.Errorf("型の誤り: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/activities", map[string]any{"nme": "x"}); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body), `項目 "nme" は使えません`) {
		t.Errorf("未知の項目: status = %d, body = %v", status, body)
	}
}
