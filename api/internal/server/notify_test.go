package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/notify"
)

// slackStub は Slack の Incoming Webhook の代わり。受け取った本文を記録する。fail なら 500 を返す。
type slackStub struct {
	mu    sync.Mutex
	texts []string
	fail  bool
	srv   *httptest.Server
}

func newSlackStub(t *testing.T) *slackStub {
	s := &slackStub{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			http.Error(w, "invalid_token", http.StatusInternalServerError)
			return
		}
		s.texts = append(s.texts, body.Text)
		io.WriteString(w, "ok")
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *slackStub) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.texts...)
}

var tokyo = time.FixedZone("JST", 9*60*60)

// notifyFixture はシナリオのフィクスチャに、時計を差し替えた通知のサービスを加える。
type notifyFixture struct {
	*scenarioFixture
	svc   *notify.Service
	slack *slackStub
	clock time.Time
}

func newNotifyFixture(t *testing.T) *notifyFixture {
	f := &notifyFixture{scenarioFixture: newScenarioFixture(t), slack: newSlackStub(t)}
	f.svc = notify.NewService(f.env.DB, notify.Options{
		SlackWebhookURL: f.slack.srv.URL,
		BaseURL:         "https://fpanda.example.com",
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:             func() time.Time { return f.clock },
	})
	// manualAct（PRJ-1）の担当は member、formulaAct（SAAS-1）も member。fn1 のマネージャーは manager1
	f.env.Exec("UPDATE users SET slack_user_id = 'U0MEMBER' WHERE id = ?", f.memberID)
	return f
}

func (f *notifyFixture) at(s string) {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, tokyo)
	if err != nil {
		panic(err)
	}
	f.clock = t
}

// notes はユーザーのお知らせのタイトルを新しい順に返す。
func (f *notifyFixture) notes(userID int64) []string {
	rows, err := f.env.Query("SELECT title FROM notifications WHERE user_id = ? ORDER BY id DESC", userID)
	if err != nil {
		f.admin.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (f *notifyFixture) setDeadline(date string) {
	if _, err := f.env.Exec("UPDATE scenarios SET update_deadline = ? WHERE id = ?", date, f.budget); err != nil {
		f.admin.t.Fatal(err)
	}
}

func TestDeadlineNotifications(t *testing.T) {
	f := newNotifyFixture(t)
	ctx := context.Background()
	// 締切は 2026-10-15（木）。既定の通知は 3日前（10/12 月）と前日（10/14 水）
	f.setDeadline("2026-10-15")

	// 更新の開始: 担当者ごとに1件。Slack はメンバー ID でメンションする。2回目は送らない
	f.at("2026-10-07 10:00")
	f.svc.UpdateStarted(ctx, f.budget)
	f.svc.UpdateStarted(ctx, f.budget)
	if got := f.notes(f.memberID); len(got) != 1 || !strings.Contains(got[0], "見込の更新が始まりました") {
		t.Fatalf("担当者のお知らせ = %v", got)
	}
	texts := f.slack.all()
	if len(texts) != 1 || !strings.Contains(texts[0], "<@U0MEMBER>（2件）") || !strings.Contains(texts[0], "10/15（木）") || !strings.Contains(texts[0], "<https://fpanda.example.com/|") {
		t.Fatalf("Slack = %v", texts)
	}

	// 締切の前: 送信時刻（9:00）より前は送らない。3日前の 9:00 を過ぎたら、未完了の担当者にだけ送る
	f.member.do("POST", f.valuesPath(f.budget, f.formulaAct)+"/complete", nil) // SAAS-1 は完了
	f.at("2026-10-12 08:59")
	if err := f.svc.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(f.slack.all()); n != 1 {
		t.Errorf("送信時刻の前に送った: %d", n)
	}
	f.at("2026-10-12 09:01")
	f.svc.RunDue(ctx)
	f.svc.RunDue(ctx) // 二重には送らない
	texts = f.slack.all()
	if len(texts) != 2 || !strings.Contains(texts[1], "あと3日") || !strings.Contains(texts[1], "<@U0MEMBER>（1件）") {
		t.Fatalf("3日前の Slack = %v", texts)
	}
	if got := f.notes(f.memberID); len(got) != 2 || !strings.Contains(got[0], "締切まであと3日です") {
		t.Errorf("3日前のお知らせ = %v", got)
	}
	// 通知の日ではない（10/13）は送らない。前日は「明日」
	f.at("2026-10-13 09:30")
	f.svc.RunDue(ctx)
	f.at("2026-10-14 09:30")
	f.svc.RunDue(ctx)
	if texts = f.slack.all(); len(texts) != 3 || !strings.Contains(texts[2], "明日") {
		t.Fatalf("前日の Slack = %v", texts)
	}

	// 締切の超過: 担当者とユニットのマネージャーに、毎日。土日は送らない
	f.at("2026-10-16 09:00")
	f.svc.RunDue(ctx)
	if got := f.notes(f.manager1ID); len(got) != 1 || !strings.Contains(got[0], "締切を1日過ぎています") {
		t.Errorf("マネージャーのお知らせ = %v", got)
	}
	var body string
	f.env.QueryRow("SELECT body FROM notifications WHERE user_id = ? ORDER BY id DESC LIMIT 1", f.manager1ID).Scan(&body)
	if !strings.Contains(body, "所管ユニットの未完了の施策（1件）") || !strings.Contains(body, "PRJ-1") {
		t.Errorf("マネージャーへの本文 = %q", body)
	}
	f.at("2026-10-17 09:00") // 土曜
	f.svc.RunDue(ctx)
	f.at("2026-10-19 09:00") // 月曜
	f.svc.RunDue(ctx)
	if texts = f.slack.all(); len(texts) != 5 || !strings.Contains(texts[4], "4日過ぎています") || !strings.Contains(texts[4], "ユニットのマネージャー") {
		t.Fatalf("超過の Slack = %v", texts)
	}

	// すべて完了すると送らない
	f.member.do("POST", f.valuesPath(f.budget, f.manualAct)+"/complete", nil)
	f.at("2026-10-20 09:00")
	f.svc.RunDue(ctx)
	if n := len(f.slack.all()); n != 5 {
		t.Errorf("完了後に送った: %d", n)
	}
}

func TestNotificationSlackFailure(t *testing.T) {
	f := newNotifyFixture(t)
	ctx := context.Background()
	f.setDeadline("2026-10-15")
	f.slack.fail = true
	f.at("2026-10-07 10:00")
	f.svc.UpdateStarted(ctx, f.budget)

	// Slack に失敗しても、アプリ内のお知らせは作る。送信の記録に失敗を残す
	if got := f.notes(f.memberID); len(got) != 1 {
		t.Errorf("お知らせ = %v", got)
	}
	runs := f.admin.mustGet("/api/notification-runs")["items"].([]any)
	run := runs[0].(map[string]any)
	if len(runs) != 1 || run["slack_status"] != "failed" || run["slack_attempts"] != float64(1) || !strings.Contains(run["slack_error"].(string), "500") {
		t.Errorf("送信の記録 = %v", runs)
	}
	if status, _ := f.member.do("GET", "/api/notification-runs", nil); status != http.StatusForbidden {
		t.Errorf("担当者の送信の記録: status = %d", status)
	}
}

func TestNotificationAPI(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	path := fmt.Sprintf("/api/scenarios/%d", f.budget)

	// 締切を入れると（作成中のシナリオ）、担当者に「更新の開始」のお知らせが届く
	status, body := a.do("PUT", path, map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "update_deadline": "2026-10-15"})
	if status != http.StatusOK || body["update_deadline"] != "2026-10-15" {
		t.Fatalf("締切の設定: status = %d, body = %v", status, body)
	}
	if status, body := a.do("PUT", path, map[string]any{"name": "2026年度 当初予算", "plan_role": "initial", "update_deadline": "10/15"}); status != http.StatusUnprocessableEntity || detail(body, "update_deadline") == "" {
		t.Errorf("締切の形式: status = %d, body = %v", status, body)
	}
	list := f.member.mustGet("/api/notifications")
	items := list["items"].([]any)
	if list["unread"] != float64(1) || len(items) != 1 || !strings.Contains(items[0].(map[string]any)["title"].(string), "見込の更新が始まりました") {
		t.Fatalf("お知らせ = %v", list)
	}
	id := int64(items[0].(map[string]any)["id"].(float64))
	// ほかの人のお知らせは既読にできない
	if status, _ := f.viewer.do("POST", fmt.Sprintf("/api/notifications/%d/read", id), nil); status != http.StatusNotFound {
		t.Errorf("ほかの人のお知らせ: status = %d", status)
	}
	if status, _ := f.member.do("POST", fmt.Sprintf("/api/notifications/%d/read", id), nil); status != http.StatusNoContent {
		t.Errorf("既読: status = %d", status)
	}
	if got := f.member.mustGet("/api/notifications")["unread"]; got != float64(0) {
		t.Errorf("既読の後の未読 = %v", got)
	}

	// 通知の設定
	settings := f.viewer.mustGet("/api/notification-settings")
	if settings["send_time"] != "09:00" || fmt.Sprint(settings["reminder_days"]) != "[3 1]" || settings["slack_configured"] != false {
		t.Errorf("既定の設定 = %v", settings)
	}
	status, body = a.do("PUT", "/api/notification-settings", map[string]any{
		"enabled_kinds": map[string]bool{"deadline_overdue": false}, "reminder_days": []int{1, 5, 1}, "send_time": "08:30", "reason": "朝に変更",
	})
	if status != http.StatusOK || fmt.Sprint(body["reminder_days"]) != "[5 1]" || body["enabled_kinds"].(map[string]any)["deadline_overdue"] != false {
		t.Errorf("設定の変更: status = %d, body = %v", status, body)
	}
	if status, body := a.do("PUT", "/api/notification-settings", map[string]any{"reminder_days": []int{30}, "send_time": "25:00"}); status != http.StatusUnprocessableEntity || detail(body, "reminder_days") == "" || detail(body, "send_time") == "" {
		t.Errorf("設定の検証: status = %d, body = %v", status, body)
	}
	if status, _ := f.member.do("PUT", "/api/notification-settings", map[string]any{"send_time": "08:30"}); status != http.StatusForbidden {
		t.Errorf("担当者の設定の変更: status = %d", status)
	}
	if status, _ := a.do("POST", "/api/notification-settings/test", nil); status != http.StatusConflict {
		t.Errorf("Slack 未設定のテスト送信: status = %d, want 409", status)
	}

	// ユーザーの Slack のメンバー ID
	if status, body := a.do("PUT", fmt.Sprintf("/api/users/%d", f.memberID), map[string]any{"name": "担当", "email": "member@example.com", "role": "member", "is_active": true, "slack_user_id": "bad id"}); status != http.StatusUnprocessableEntity || detail(body, "slack_user_id") == "" {
		t.Errorf("メンバー ID の形式: status = %d, body = %v", status, body)
	}
	if status, body := a.do("PUT", fmt.Sprintf("/api/users/%d", f.memberID), map[string]any{"name": "担当", "email": "member@example.com", "role": "member", "is_active": true, "slack_user_id": "U012AB3CD"}); status != http.StatusOK || body["slack_user_id"] != "U012AB3CD" {
		t.Errorf("メンバー ID の登録: status = %d, body = %v", status, body)
	}
}

func TestActualsReflectedNotification(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	// 前回見込: 4月 1,200。今回（作成中）は前回見込を複製し、4月の実績 900 を決算確定月で反映する（差 25%）
	prev := a.mustCreate("/api/scenarios", map[string]any{"name": "9月見込", "fiscal_year": 2026, "base_scenario_id": f.budget})
	if status, body := a.do("PUT", f.valuesPath(prev, f.manualAct)+"/amounts", map[string]any{"reason": "r", "amounts": []map[string]any{{"subject_id": f.sales, "target_month": "2026-04", "amount": 1200}}}); status != http.StatusOK {
		t.Fatalf("amounts: status = %d, body = %v", status, body)
	}
	cur := a.mustCreate("/api/scenarios", map[string]any{"name": "10月見込", "fiscal_year": 2026, "base_scenario_id": prev})
	a.do("POST", fmt.Sprintf("/api/scenarios/%d/activate", cur), nil)
	a.upload("/api/actuals/import", "target_month,box_code,account_code,amount\n2026-04,PRJ-1,4110,900\n", "4月実績")
	status, body := a.do("PUT", fmt.Sprintf("/api/scenarios/%d", cur), map[string]any{"name": "10月見込", "actual_through": "2026-04", "previous_scenario_id": prev, "reason": "4月決算確定"})
	if status != http.StatusOK {
		t.Fatalf("決算確定月: status = %d, body = %v", status, body)
	}
	items := f.member.mustGet("/api/notifications")["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("お知らせ = %v", items)
	}
	n := items[0].(map[string]any)
	if !strings.Contains(n["title"].(string), "4月の実績が反映されました") || !strings.Contains(n["body"].(string), "PRJ-1") || strings.Contains(n["body"].(string), "SAAS-1") {
		t.Errorf("実績の反映のお知らせ = %v", n)
	}
}
