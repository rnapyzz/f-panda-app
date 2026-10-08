package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestManualReminder は FP&A のホームの「催促する」（docs/plan.md「2.20」）を確かめる。
func TestManualReminder(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	path := fmt.Sprintf("/api/scenarios/%d/reminders", f.budget)

	// member は PRJ-1・SAAS-1 の担当（どちらも未完了）
	status, body := a.do("POST", path, map[string]any{"user_id": f.memberID})
	if status != http.StatusOK || body["activities"] != float64(2) || body["slack"] != "off" {
		t.Fatalf("催促: status = %d, body = %v", status, body)
	}
	notes := f.member.mustGet("/api/notifications")["items"].([]any)
	if n := notes[0].(map[string]any); !strings.Contains(n["title"].(string), "見込の更新をお願いします") || !strings.Contains(n["body"].(string), "PRJ-1") || n["kind"] != "manual_reminder" {
		t.Errorf("お知らせ = %v", n)
	}
	// 同じ人には1日1回まで
	if status, _ := a.do("POST", path, map[string]any{"user_id": f.memberID}); status != http.StatusConflict {
		t.Errorf("2回目: status = %d", status)
	}
	if got := fmt.Sprint(a.mustGet(path)["user_ids"]); got != fmt.Sprintf("[%d]", f.memberID) {
		t.Errorf("今日催促した人 = %s", got)
	}
	// 担当する未完了の施策がない人
	if status, body := a.do("POST", path, map[string]any{"user_id": f.manager1ID}); status != http.StatusUnprocessableEntity || detail(body, "user_id") == "" {
		t.Errorf("未完了の施策なし: status = %d, body = %v", status, body)
	}
	// FP&A のみ。作成中のシナリオだけ
	if status, _ := f.member.do("POST", path, map[string]any{"user_id": f.memberID}); status != http.StatusForbidden {
		t.Errorf("現場担当: status = %d", status)
	}
	other := a.mustCreate("/api/scenarios", map[string]any{"name": "比較用", "fiscal_year": 2026})
	if status, _ := a.do("POST", fmt.Sprintf("/api/scenarios/%d/reminders", other), map[string]any{"user_id": f.memberID}); status != http.StatusConflict {
		t.Errorf("作成中以外のシナリオ: status = %d", status)
	}
}
