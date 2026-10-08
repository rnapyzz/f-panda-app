package notify

// 催促（docs/plan.md「2.20」）。FP&A がホームの「催促する」で、未完了の施策の担当者に1人ずつ送る。

import (
	"context"
	"fmt"
	"net/http"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// sinceToday は、今日（日本時間）の0時から今までの秒数。DB の NOW() と比べて「今日送ったか」を判定する（時刻のずれを避ける）。
func (s *Service) sinceToday() int64 {
	return int64(s.now().Sub(s.today()).Seconds())
}

// remindedToday は、シナリオで今日催促した人の ID を返す。
func (s *Service) remindedToday(ctx context.Context, scenarioID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT user_id FROM notifications
		WHERE kind = ? AND scenario_id = ? AND created_at >= NOW() - INTERVAL ? SECOND ORDER BY user_id`,
		KindManualReminder, scenarioID, s.sinceToday())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// listReminders は GET /api/scenarios/{id}/reminders。今日催促した人。
func (s *Service) listReminders(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	ids, err := s.remindedToday(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user_ids": ids})
	return nil
}

// sendReminder は POST /api/scenarios/{id}/reminders（user_id）。
// 作成中のシナリオで、その人が担当する未完了の施策の一覧を、お知らせと Slack（メンション）で送る。同じ人には1日1回まで。
func (s *Service) sendReminder(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	sc, err := s.findScenario(ctx, id)
	if err != nil {
		return httpx.NotFound("シナリオが見つかりません")
	}
	if !sc.active || sc.locked {
		return httpx.Conflict("催促できるのは、今回の見込（作成中のシナリオ）だけです")
	}
	sent, err := s.remindedToday(ctx, id)
	if err != nil {
		return err
	}
	for _, u := range sent {
		if u == req.UserID {
			return httpx.Conflict("この人には今日すでに催促しています（1日1回まで）")
		}
	}
	items, people, err := s.loadActivities(ctx, id)
	if err != nil {
		return err
	}
	var open []activityItem
	for _, it := range items {
		if it.owner == req.UserID && !it.completed {
			open = append(open, it)
		}
	}
	p, ok := people[req.UserID]
	if !ok || len(open) == 0 {
		return httpx.Validation(map[string]string{"user_id": "この人が担当する未完了の施策はありません"})
	}

	deadline := ""
	if sc.deadline != nil {
		deadline = fmt.Sprintf("締切は %s です。", dateLabel(*sc.deadline))
	}
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO notifications (user_id, kind, scenario_id, title, body, link) VALUES (?, ?, ?, ?, ?, ?)",
		p.id, KindManualReminder, id, fmt.Sprintf("見込の更新をお願いします（%s）", sc.name),
		fmt.Sprintf("%s未完了の施策（%d件）を更新し、今回の見込の説明を書いて「説明して完了」を押してください。\n%s", deadline, len(open), listActivities(open)),
		"/"); err != nil {
		return err
	}
	slack := "off"
	if s.SlackConfigured() {
		slack = "sent"
		text := fmt.Sprintf("【%s】%s さん、見込の更新をお願いします。%s未完了 %d件%s", sc.name, mention(p), deadline, len(open), s.link("/", "ホームを開く"))
		if err := s.postSlack(ctx, text); err != nil {
			s.logger.Error("notify manual_reminder slack", "scenario_id", id, "user_id", p.id, "error", err)
			slack = "failed"
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user_id": p.id, "activities": len(open), "slack": slack})
	return nil
}
