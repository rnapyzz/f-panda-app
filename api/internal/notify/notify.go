// Package notify は締切と通知を扱う（docs/plan.md「2.13 締切と通知」）。
//
// 通知の種類は4つ（更新の開始・締切の前・締切の超過・実績の反映）。宛先ごとにアプリ内のお知らせ（notifications）を作り、
// Slack の共有チャンネルにも1回の投稿にまとめて送る（Incoming Webhook。環境変数 SLACK_WEBHOOK_URL が空なら送らない）。
// 送信の記録（notification_runs）を（種類・シナリオ・日付）で一意に残し、同じ通知を同じ日に二重に送らない。
package notify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
)

// 通知の種類。
const (
	KindUpdateStarted    = "update_started"
	KindDeadlineReminder = "deadline_reminder"
	KindDeadlineOverdue  = "deadline_overdue"
	KindActualsReflected = "actuals_reflected"
	// KindManualReminder は FP&A がホームから送る催促（docs/plan.md「2.20」）。設定の有効・無効の対象ではない
	KindManualReminder = "manual_reminder"
)

var kinds = []string{KindUpdateStarted, KindDeadlineReminder, KindDeadlineOverdue, KindActualsReflected}

const (
	maxSlackAttempts = 3                // 同じ日のうちに Slack へ送る回数の上限
	slackRetryAfter  = 10 * time.Minute // 失敗した Slack の送信を送り直すまでの間隔
	slackTimeout     = 10 * time.Second
	keepDays         = 180 // アプリ内のお知らせを残す日数
)

// Options は Service の設定。
type Options struct {
	// SlackWebhookURL は Slack の Incoming Webhook の URL。空なら Slack には送らない
	SlackWebhookURL string
	// BaseURL は Slack の本文に載せるアプリの URL（例: https://fpanda.example.com）。空ならリンクを載せない
	BaseURL string
	Logger  *slog.Logger
	// Now はテスト用の時計。nil なら time.Now
	Now func() time.Time
}

// Service は通知を作って送る。
type Service struct {
	db      *sql.DB
	slack   string
	baseURL string
	client  *http.Client
	logger  *slog.Logger
	now     func() time.Time
}

// NewService は Service を作る。
func NewService(db *sql.DB, o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{db: db, slack: o.SlackWebhookURL, baseURL: strings.TrimRight(o.BaseURL, "/"), client: &http.Client{Timeout: slackTimeout}, logger: o.Logger, now: o.Now}
}

var jst = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	return loc
}()

func (s *Service) today() time.Time {
	t := s.now().In(jst)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, jst)
}

// message は1回の通知の内容。宛先ごとのお知らせと、Slack の本文。
type message struct {
	kind       string
	scenarioID int64
	runDate    time.Time
	notes      []note
	slackText  string
}

// note は1人へのお知らせ。
type note struct {
	userID int64
	title  string
	body   string
	link   string
}

// deliver は送信の記録を作り、お知らせを登録して Slack に送る。
// 同じ（種類・シナリオ・日付）の記録があれば、お知らせは作らず、失敗した Slack の送信だけを送り直す。
func (s *Service) deliver(ctx context.Context, m message) error {
	if len(m.notes) == 0 {
		return nil
	}
	date := m.runDate.Format("2006-01-02")
	var runID int64
	var status string
	var attempts int
	var waited int64 // 前回の送信からの秒数（DB の時計で測る）
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slack_status, slack_attempts, TIMESTAMPDIFF(SECOND, updated_at, NOW())
		FROM notification_runs WHERE kind = ? AND scenario_id = ? AND run_date = ?`,
		m.kind, m.scenarioID, date).Scan(&runID, &status, &attempts, &waited)
	switch {
	case err == nil:
		if status == "failed" && s.slack != "" && attempts < maxSlackAttempts && time.Duration(waited)*time.Second >= slackRetryAfter {
			return s.retrySlack(ctx, runID, attempts)
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO notification_runs (kind, scenario_id, run_date, recipients, slack_text) VALUES (?, ?, ?, ?, ?)",
		m.kind, m.scenarioID, date, len(m.notes), m.slackText)
	if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
		return nil // ほかの API サーバーが先に送った
	}
	if err != nil {
		return err
	}
	runID, _ = res.LastInsertId()
	for _, n := range m.notes {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO notifications (user_id, kind, scenario_id, title, body, link) VALUES (?, ?, ?, ?, ?, ?)",
			n.userID, m.kind, m.scenarioID, n.title, n.body, n.link); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.slack == "" {
		return nil
	}
	return s.retrySlack(ctx, runID, 0)
}

// retrySlack は記録の Slack の本文を送り、結果を記録する。
func (s *Service) retrySlack(ctx context.Context, runID int64, attempts int) error {
	var text string
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(slack_text, '') FROM notification_runs WHERE id = ?", runID).Scan(&text); err != nil {
		return err
	}
	sendErr := s.postSlack(ctx, text)
	status, msg := "sent", sql.NullString{}
	if sendErr != nil {
		status, msg = "failed", sql.NullString{String: sendErr.Error(), Valid: true}
		s.logger.Error("slack notification failed", "run_id", runID, "error", sendErr)
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE notification_runs SET slack_status = ?, slack_attempts = ?, slack_error = ? WHERE id = ?", status, attempts+1, msg, runID)
	return err
}

// postSlack は Incoming Webhook に本文を送る。
func (s *Service) postSlack(ctx context.Context, text string) error {
	if s.slack == "" {
		return errors.New("Slack の Webhook URL が設定されていません")
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.slack, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Slack に送れませんでした: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("Slack が %d を返しました: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// SlackConfigured は Slack の Webhook URL が設定されているかを返す。
func (s *Service) SlackConfigured() bool { return s.slack != "" }

// link は Slack の本文に載せるリンク（<URL|ラベル>）。BaseURL がなければ空。
func (s *Service) link(path, label string) string {
	if s.baseURL == "" {
		return ""
	}
	return fmt.Sprintf(" <%s%s|%s>", s.baseURL, path, label)
}

// cleanup は古いお知らせを消す。
func (s *Service) cleanup(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM notifications WHERE created_at < ?", s.now().In(jst).AddDate(0, 0, -keepDays))
	return err
}
