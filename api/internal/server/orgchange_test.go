package server_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/orgchange"
)

func (f *activityFixture) unitOf(activityID int64) int64 {
	f.admin.t.Helper()
	var unit int64
	if err := f.env.QueryRow("SELECT unit_id FROM activities WHERE id = ?", activityID).Scan(&unit); err != nil {
		f.admin.t.Fatal(err)
	}
	return unit
}

func tomorrow() string { return time.Now().In(tokyo).AddDate(0, 0, 1).Format("2006-01-02") }

// TestOrgChangePlan は docs/plan.md「2.15」の組織変更の予約を確かめる。
func TestOrgChangePlan(t *testing.T) {
	f := newActivityFixture(t)
	a := f.admin
	act1 := a.mustCreate("/api/activities", activityBody(f.fn1, "ACT-A", map[string]any{"owner_user_id": f.memberID}))
	act2 := a.mustCreate("/api/activities", activityBody(f.fn1, "ACT-B", nil))
	seg2 := a.mustCreate("/api/segments", map[string]any{"name": "新事業"})
	var org int64
	f.env.QueryRow("SELECT organization_id FROM units WHERE id = ?", f.fn2).Scan(&org)

	items := []map[string]any{
		{"kind": "move_activity", "activity_id": act1, "target_unit_id": f.fn2},
		{"kind": "move_unit", "unit_id": f.fn2, "segment_id": seg2, "organization_id": org},
		{"kind": "change_owner", "activity_id": act2, "owner_user_id": f.memberID},
	}
	// 有効日は明日以降。今の状態で適用できない予約は作れない
	if status, body := a.do("POST", "/api/org-change-plans", map[string]any{"name": "10月の組織変更", "effective_date": time.Now().In(tokyo).Format("2006-01-02"), "items": items}); status != http.StatusUnprocessableEntity || detail(body, "effective_date") == "" {
		t.Errorf("今日の有効日: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", "/api/org-change-plans", map[string]any{"name": "x", "effective_date": tomorrow(), "items": []map[string]any{{"kind": "move_activity", "activity_id": act1, "target_unit_id": 99999}}}); status != http.StatusUnprocessableEntity || !strings.Contains(detail(body, "items"), "1 件目") {
		t.Errorf("存在しない移動先: status = %d, body = %v", status, body)
	}
	if status, _ := f.manager1.do("GET", "/api/org-change-plans", nil); status != http.StatusForbidden {
		t.Errorf("マネージャーの一覧: status = %d", status)
	}
	plan := a.mustCreate("/api/org-change-plans", map[string]any{"name": "10月の組織変更", "effective_date": tomorrow(), "items": items})
	// 作っただけでは変わらない
	if f.unitOf(act1) != f.fn1 {
		t.Fatal("予約しただけで施策が移った")
	}

	// 有効日が来ると自動で適用する（時計を明日にした Service で確かめる）
	svc := orgchange.NewService(f.env.DB, orgchange.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return time.Now().AddDate(0, 0, 1) }})
	if err := svc.ApplyDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := a.mustGet(fmt.Sprintf("/api/org-change-plans/%d", plan))
	if got["status"] != "applied" || f.unitOf(act1) != f.fn2 {
		t.Fatalf("適用後: plan = %v, unit = %d", got, f.unitOf(act1))
	}
	var seg int64
	f.env.QueryRow("SELECT segment_id FROM units WHERE id = ?", f.fn2).Scan(&seg)
	if seg != seg2 {
		t.Errorf("ユニットの所属 = %d, want %d", seg, seg2)
	}
	// 適用は予約を作った人の変更セットとして残る
	var n int
	f.env.QueryRow(`SELECT COUNT(*) FROM change_sets WHERE reason = '組織変更の予約「10月の組織変更」を適用' AND user_id = ?`, f.env.adminID).Scan(&n)
	if n != 1 {
		t.Errorf("適用の変更セット = %d", n)
	}
	// 適用済みは変更・削除できない
	if status, _ := a.do("DELETE", fmt.Sprintf("/api/org-change-plans/%d", plan), nil); status != http.StatusConflict {
		t.Errorf("適用済みの削除: status = %d", status)
	}
}

func TestOrgChangePlanFailure(t *testing.T) {
	f := newActivityFixture(t)
	a := f.admin
	act := a.mustCreate("/api/activities", activityBody(f.fn1, "ACT-A", nil))
	plan := a.mustCreate("/api/org-change-plans", map[string]any{"name": "統合の予約", "effective_date": tomorrow(), "items": []map[string]any{
		{"kind": "move_activity", "activity_id": act, "target_unit_id": f.fn2},
	}})
	// 予約の後に移動先が廃止された（fn2 の施策はないので廃止できる）
	if status, body := a.do("POST", fmt.Sprintf("/api/units/%d/archive", f.fn2), nil); status != http.StatusNoContent {
		t.Fatalf("廃止: status = %d, body = %v", status, body)
	}
	status, body := a.do("POST", fmt.Sprintf("/api/org-change-plans/%d/apply", plan), nil)
	if status != http.StatusUnprocessableEntity || !strings.Contains(detail(body, "items"), "廃止") {
		t.Fatalf("今すぐ適用: status = %d, body = %v", status, body)
	}
	got := a.mustGet(fmt.Sprintf("/api/org-change-plans/%d", plan))
	if got["status"] != "failed" || f.unitOf(act) != f.fn1 {
		t.Errorf("失敗の後: plan = %v, unit = %d（何も変えない）", got, f.unitOf(act))
	}
	// FP&A にお知らせが届く
	items := a.mustGet("/api/notifications")["items"].([]any)
	if len(items) != 1 || !strings.Contains(items[0].(map[string]any)["title"].(string), "統合の予約") {
		t.Errorf("失敗のお知らせ = %v", items)
	}
	// 直して（廃止を取り消して）今すぐ適用する
	a.do("POST", fmt.Sprintf("/api/units/%d/unarchive", f.fn2), nil)
	if status, body := a.do("POST", fmt.Sprintf("/api/org-change-plans/%d/apply", plan), nil); status != http.StatusOK || body["status"] != "applied" {
		t.Errorf("直してから適用: status = %d, body = %v", status, body)
	}
}

func TestMergeUnitAndDeactivate(t *testing.T) {
	f := newActivityFixture(t)
	a := f.admin
	act := a.mustCreate("/api/activities", activityBody(f.fn1, "ACT-A", map[string]any{"owner_user_id": f.memberID}))

	// 施策が所属しているユニットは廃止できない。統合なら施策を移して廃止にする
	if status, _ := a.do("POST", fmt.Sprintf("/api/units/%d/archive", f.fn1), nil); status != http.StatusConflict {
		t.Errorf("施策のあるユニットの廃止: status = %d", status)
	}
	if status, body := a.do("POST", fmt.Sprintf("/api/units/%d/merge", f.fn1), map[string]any{"target_unit_id": f.fn2}); status != http.StatusUnprocessableEntity || detail(body, "reason") == "" {
		t.Errorf("理由なしの統合: status = %d, body = %v", status, body)
	}
	if status, body := a.do("POST", fmt.Sprintf("/api/units/%d/merge", f.fn1), map[string]any{"target_unit_id": f.fn2, "reason": "課の統合"}); status != http.StatusOK || body["moved"] != float64(1) {
		t.Fatalf("統合: status = %d, body = %v", status, body)
	}
	if f.unitOf(act) != f.fn2 {
		t.Errorf("統合後の施策のユニット = %d", f.unitOf(act))
	}
	// 廃止したユニットは一覧に出さず、施策を所属させられない
	if items := a.mustGet("/api/units")["items"].([]any); len(items) != 1 {
		t.Errorf("一覧（廃止を除く） = %d 件", len(items))
	}
	if items := a.mustGet("/api/units?include_archived=true")["items"].([]any); len(items) != 2 {
		t.Errorf("一覧（廃止を含む） = %d 件", len(items))
	}
	if status, body := a.do("POST", "/api/activities", activityBody(f.fn1, "ACT-X", nil)); status != http.StatusUnprocessableEntity || !strings.Contains(detail(body, "unit_id"), "廃止") {
		t.Errorf("廃止したユニットへの施策の作成: status = %d, body = %v", status, body)
	}

	// 無効化のときの後任者: 担当の施策と所管ユニットをまとめて付け替えてから無効にする
	counts := a.mustGet(fmt.Sprintf("/api/users/%d/assignments", f.memberID))
	if counts["activities"] != float64(1) || counts["units"] != float64(0) {
		t.Errorf("担当の件数 = %v", counts)
	}
	if status, body := a.do("POST", fmt.Sprintf("/api/users/%d/deactivate", f.memberID), map[string]any{"successor_user_id": f.memberID}); status != http.StatusUnprocessableEntity || detail(body, "successor_user_id") == "" {
		t.Errorf("本人を後任者に: status = %d, body = %v", status, body)
	}
	status, body := a.do("POST", fmt.Sprintf("/api/users/%d/deactivate", f.memberID), map[string]any{"successor_user_id": f.manager1ID, "reason": "異動"})
	if status != http.StatusOK || body["moved"].(map[string]any)["activities"] != float64(1) {
		t.Fatalf("後任者への付け替え: status = %d, body = %v", status, body)
	}
	var owner int64
	var active bool
	f.env.QueryRow("SELECT owner_user_id FROM activities WHERE id = ?", act).Scan(&owner)
	f.env.QueryRow("SELECT is_active FROM users WHERE id = ?", f.memberID).Scan(&active)
	if owner != f.manager1ID || active {
		t.Errorf("付け替え後: owner = %d, active = %v", owner, active)
	}
	if status, _ := f.member.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("無効にしたユーザーのセッション: status = %d", status)
	}

	// CSV で無効にしたユーザーに担当が残っていれば知らせる
	status, body = a.upload("/api/users/import", "email,name,role,is_active\nmanager1@example.com,マネージャー1,manager,false\n", "異動")
	warnings, _ := body["warnings"].([]any)
	if status != http.StatusOK || len(warnings) != 1 || !strings.Contains(warnings[0].(string), "担当の施策 1 件") {
		t.Errorf("CSV の注意: status = %d, body = %v", status, body)
	}
}
