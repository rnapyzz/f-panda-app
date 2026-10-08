package notify

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// Register はルートを登録する。requireAdmin は FP&A のみに制限するミドルウェア。
func (s *Service) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler, requireAdmin func(http.Handler) http.Handler) {
	read := func(f httpx.HandlerFunc) http.Handler { return requireAuth(httpx.Handle(f)) }
	write := func(f httpx.HandlerFunc) http.Handler { return requireAuth(requireAdmin(httpx.Handle(f))) }

	mux.Handle("GET /api/notifications", read(s.listNotifications))
	mux.Handle("POST /api/notifications/read-all", read(s.readAll))
	mux.Handle("POST /api/notifications/{id}/read", read(s.readOne))
	mux.Handle("GET /api/notification-settings", read(s.getSettings))
	mux.Handle("PUT /api/notification-settings", write(s.putSettings))
	mux.Handle("POST /api/notification-settings/test", write(s.testSlack))
	mux.Handle("GET /api/notification-runs", write(s.listRuns))
	mux.Handle("GET /api/scenarios/{id}/reminders", write(s.listReminders))
	mux.Handle("POST /api/scenarios/{id}/reminders", write(s.sendReminder))
}

func currentUser(r *http.Request) (auth.User, error) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return auth.User{}, httpx.Unauthorized("ログインしてください")
	}
	return u, nil
}

func limitOf(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, max)
}

// notification はアプリ内のお知らせ。
type notification struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	ScenarioID *int64     `json:"scenario_id"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Link       string     `json:"link"`
	CreatedAt  time.Time  `json:"created_at"`
	ReadAt     *time.Time `json:"read_at"`
}

// listNotifications は GET /api/notifications?limit=。自分宛てのお知らせ（新しい順）と未読の数。
func (s *Service) listNotifications(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, scenario_id, title, body, link, created_at, read_at
		FROM notifications WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, u.ID, limitOf(r, 30, 100))
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []notification{}
	for rows.Next() {
		var n notification
		var scenario sql.NullInt64
		var read sql.NullTime
		if err := rows.Scan(&n.ID, &n.Kind, &scenario, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &read); err != nil {
			return err
		}
		if scenario.Valid {
			n.ScenarioID = &scenario.Int64
		}
		if read.Valid {
			n.ReadAt = &read.Time
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var unread int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE user_id = ? AND read_at IS NULL", u.ID).Scan(&unread); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "unread": unread})
	return nil
}

// readOne は POST /api/notifications/{id}/read。自分宛てのお知らせを既読にする。
func (s *Service) readOne(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(r.Context(), "UPDATE notifications SET read_at = COALESCE(read_at, NOW()) WHERE id = ? AND user_id = ?", id, u.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var one int
		if err := s.db.QueryRowContext(r.Context(), "SELECT 1 FROM notifications WHERE id = ? AND user_id = ?", id, u.ID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("お知らせが見つかりません")
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// readAll は POST /api/notifications/read-all。
func (s *Service) readAll(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(r.Context(), "UPDATE notifications SET read_at = NOW() WHERE user_id = ? AND read_at IS NULL", u.ID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type settingsResponse struct {
	settings
	SlackConfigured bool `json:"slack_configured"`
}

// getSettings は GET /api/notification-settings。
func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.loadSettings(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, settingsResponse{settings: st, SlackConfigured: s.SlackConfigured()})
	return nil
}

type settingsRequest struct {
	EnabledKinds map[string]bool `json:"enabled_kinds"`
	ReminderDays []int           `json:"reminder_days"`
	SendTime     string          `json:"send_time"`
	Reason       string          `json:"reason"`
}

var timePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// putSettings は PUT /api/notification-settings。FP&A のみ。変更は変更履歴に残す。
func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	var req settingsRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	enabled := map[string]bool{}
	for k, on := range req.EnabledKinds {
		if !slices.Contains(kinds, k) {
			v.Add("enabled_kinds", "通知の種類 "+k+" はありません")
		}
		enabled[k] = on
	}
	for _, k := range kinds {
		if _, ok := enabled[k]; !ok {
			enabled[k] = true
		}
	}
	days := slices.Clone(req.ReminderDays)
	slices.Sort(days)
	days = slices.Compact(days)
	slices.Reverse(days)
	if len(days) > 5 {
		v.Add("reminder_days", "締切の前の通知は5回までです")
	}
	for _, d := range days {
		if d < 1 || d > 14 {
			v.Add("reminder_days", "締切の前の日数は 1〜14 日で指定してください")
			break
		}
	}
	if !timePattern.MatchString(req.SendTime) {
		v.Add("send_time", "送信時刻は HH:MM（例: 09:00）で指定してください")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	var after settings
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := s.loadSettings(ctx)
		if err != nil {
			return err
		}
		strs := make([]string, len(days))
		for i, d := range days {
			strs[i] = strconv.Itoa(d)
		}
		js, _ := json.Marshal(enabled)
		if _, err := tx.ExecContext(ctx, "UPDATE notification_settings SET enabled_kinds = ?, reminder_days = ?, send_time = ? WHERE id = 1",
			string(js), strings.Join(strs, ","), req.SendTime+":00"); err != nil {
			return err
		}
		after = settings{EnabledKinds: enabled, ReminderDays: days, SendTime: req.SendTime}
		if after.ReminderDays == nil {
			after.ReminderDays = []int{}
		}
		return rec.Update(ctx, "notification_settings", 1, before, after)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, settingsResponse{settings: after, SlackConfigured: s.SlackConfigured()})
	return nil
}

// testSlack は POST /api/notification-settings/test。Slack にテスト送信する。
func (s *Service) testSlack(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	if !s.SlackConfigured() {
		return httpx.Conflict("Slack の Webhook URL（環境変数 SLACK_WEBHOOK_URL）が設定されていません")
	}
	if err := s.postSlack(r.Context(), fmt.Sprintf("F-Panda からのテスト送信です（%s さんが送信）。", u.Name)); err != nil {
		return &httpx.Error{Status: http.StatusBadGateway, Code: "slack_failed", Message: err.Error()}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type run struct {
	ID            int64     `json:"id"`
	Kind          string    `json:"kind"`
	ScenarioID    int64     `json:"scenario_id"`
	ScenarioName  string    `json:"scenario_name"`
	RunDate       string    `json:"run_date"`
	Recipients    int       `json:"recipients"`
	SlackStatus   string    `json:"slack_status"`
	SlackAttempts int       `json:"slack_attempts"`
	SlackError    string    `json:"slack_error"`
	CreatedAt     time.Time `json:"created_at"`
}

// listRuns は GET /api/notification-runs?limit=。FP&A のみ。送信の記録（新しい順）。
func (s *Service) listRuns(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT n.id, n.kind, n.scenario_id, s.name, DATE_FORMAT(n.run_date, '%Y-%m-%d'), n.recipients, n.slack_status, n.slack_attempts,
		       COALESCE(n.slack_error, ''), n.created_at
		FROM notification_runs n JOIN scenarios s ON s.id = n.scenario_id
		ORDER BY n.created_at DESC, n.id DESC LIMIT ?`, limitOf(r, 50, 200))
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []run{}
	for rows.Next() {
		var x run
		if err := rows.Scan(&x.ID, &x.Kind, &x.ScenarioID, &x.ScenarioName, &x.RunDate, &x.Recipients, &x.SlackStatus, &x.SlackAttempts, &x.SlackError, &x.CreatedAt); err != nil {
			return err
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}
