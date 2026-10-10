package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// commentNotices は自分宛ての、コメントのお知らせ（kind = comment）を返す。
func commentNotices(c *client) []map[string]any {
	var out []map[string]any
	for _, it := range c.mustGet("/api/notifications")["items"].([]any) {
		if m := it.(map[string]any); m["kind"] == "comment" {
			out = append(out, m)
		}
	}
	return out
}

// TestComments は説明へのコメント（docs/plan.md「2.24」）を確かめる。
func TestComments(t *testing.T) {
	f := newScenarioFixture(t)
	path := f.valuesPath(f.budget, f.manualAct) + "/comments"
	post := func(c *client, body string) (int, map[string]any) {
		return c.do("POST", path, map[string]any{"body": body})
	}

	// マネージャーが書く → 担当者（member）にお知らせ。書いた本人には送らない
	if status, body := post(f.manager1, "単価を据え置いた理由も書いてください"); status != http.StatusCreated {
		t.Fatalf("投稿: %d %v", status, body)
	}
	notices := commentNotices(f.member)
	if len(notices) != 1 || !strings.Contains(notices[0]["title"].(string), "コメントしました") {
		t.Fatalf("担当者へのお知らせ = %v", notices)
	}
	if want := fmt.Sprintf("/activities/%d?tab=update&scenario=%d", f.manualAct, f.budget); notices[0]["link"] != want {
		t.Errorf("リンク = %v, want %s", notices[0]["link"], want)
	}
	if n := commentNotices(f.manager1); len(n) != 0 {
		t.Errorf("書いた本人へのお知らせ = %v", n)
	}

	// 担当者が返信 → すでに書いたマネージャーにお知らせ。閲覧者（経営陣）も書ける
	if status, body := post(f.member, "値上げは来期の予定のためです"); status != http.StatusCreated {
		t.Fatalf("返信: %d %v", status, body)
	}
	if n := commentNotices(f.manager1); len(n) != 1 {
		t.Errorf("マネージャーへのお知らせ = %d件, want 1", len(n))
	}
	status, body := post(f.viewer, "了解です")
	if status != http.StatusCreated {
		t.Fatalf("閲覧者の投稿: %d %v", status, body)
	}
	items := body["items"].([]any)
	if len(items) != 3 || body["can_post"] != true {
		t.Fatalf("一覧 = %v", body)
	}
	if n := len(commentNotices(f.member)); n != 2 {
		t.Errorf("担当者へのお知らせ = %d件, want 2（マネージャー・閲覧者）", n)
	}

	// コメントは更新の状態を変えない
	if note := f.member.mustGet(f.valuesPath(f.budget, f.manualAct) + "/note"); note["status"] != "not_started" {
		t.Errorf("状態 = %v, want not_started", note["status"])
	}

	// 入力の誤り
	for _, b := range []string{"  ", strings.Repeat("あ", 1001)} {
		if status, body := post(f.member, b); status != http.StatusUnprocessableEntity || detail(body, "body") == "" {
			t.Errorf("本文 %d 文字: %d %v", len([]rune(b)), status, body)
		}
	}

	// 消せるのは書いた本人だけ。消したものは本文を返さない
	first := items[0].(map[string]any)
	cid := int64(first["id"].(float64))
	if status, _ := f.member.do("DELETE", fmt.Sprintf("%s/%d", path, cid), nil); status != http.StatusForbidden {
		t.Errorf("他人のコメントの削除: status = %d, want 403", status)
	}
	status, body = f.manager1.do("DELETE", fmt.Sprintf("%s/%d", path, cid), nil)
	if status != http.StatusOK {
		t.Fatalf("削除: %d %v", status, body)
	}
	if c := body["items"].([]any)[0].(map[string]any); c["deleted"] != true || c["body"] != "" || c["can_delete"] != false {
		t.Errorf("消したコメント = %v", c)
	}

	// 件数（消したものを除く）: ホームとリスク
	for _, it := range f.admin.mustGet(fmt.Sprintf("/api/scenarios/%d/activity-status?scope=all", f.budget))["items"].([]any) {
		if m := it.(map[string]any); m["code"] == "PRJ-1" && m["comment_count"] != 2.0 {
			t.Errorf("ホームの件数 = %v, want 2", m["comment_count"])
		}
	}
	for _, it := range f.admin.mustGet(fmt.Sprintf("/api/reports/risk?scenario_id=%d", f.budget))["activities"].([]any) {
		if m := it.(map[string]any); m["code"] == "PRJ-1" && m["comment_count"] != 2.0 {
			t.Errorf("リスクの件数 = %v, want 2", m["comment_count"])
		}
	}

	// ロック済みのシナリオには書けない・消せない（読める）
	if status, body := f.admin.do("POST", fmt.Sprintf("/api/scenarios/%d/lock", f.budget), map[string]any{"reason": "確定"}); status != http.StatusOK {
		t.Fatalf("lock: %d %v", status, body)
	}
	if status, _ := post(f.member, "追記"); status != http.StatusConflict {
		t.Errorf("ロック後の投稿: status = %d, want 409", status)
	}
	second := items[1].(map[string]any)
	if status, _ := f.member.do("DELETE", fmt.Sprintf("%s/%v", path, second["id"]), nil); status != http.StatusConflict {
		t.Errorf("ロック後の削除: status = %d, want 409", status)
	}
	if body := f.member.mustGet(path); body["can_post"] != false || len(body["items"].([]any)) != 3 {
		t.Errorf("ロック後の一覧 = %v", body)
	}
}
