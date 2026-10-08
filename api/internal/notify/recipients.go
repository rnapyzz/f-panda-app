package notify

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/rnapyzz/f-panda-app/api/internal/target"
	"sort"
	"strings"
	"time"
)

// scenarioInfo は通知の対象のシナリオ。
type scenarioInfo struct {
	id       int64
	name     string
	active   bool
	locked   bool
	deadline *time.Time
}

func (s *Service) findScenario(ctx context.Context, id int64) (scenarioInfo, error) {
	var sc scenarioInfo
	var deadline sql.NullTime
	err := s.db.QueryRowContext(ctx, "SELECT id, name, is_active, is_locked, update_deadline FROM scenarios WHERE id = ?", id).
		Scan(&sc.id, &sc.name, &sc.active, &sc.locked, &deadline)
	if deadline.Valid {
		d := time.Date(deadline.Time.Year(), deadline.Time.Month(), deadline.Time.Day(), 0, 0, 0, 0, jst)
		sc.deadline = &d
	}
	return sc, err
}

// activeScenario は作成中で、締切があり、ロックされていないシナリオを返す（なければ nil）。
func (s *Service) activeScenario(ctx context.Context) (*scenarioInfo, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM scenarios WHERE is_active AND NOT is_locked AND update_deadline IS NOT NULL").Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sc, err := s.findScenario(ctx, id)
	return &sc, err
}

// activityItem は通知に載せる施策。
type activityItem struct {
	id, unitID int64
	code, name string
	assignee   int64 // 担当者（いなければユニットのマネージャー）。0 は宛先なし
	owner      int64 // 施策の担当者（有効なユーザー）。0 はなし
	manager    int64 // ユニットのマネージャー。0 はなし
	completed  bool
}

// person は宛先の人。
type person struct {
	id      int64
	name    string
	slackID string
}

// loadActivities は、シナリオの更新の対象の施策（docs/plan.md「2.10」）と更新の状態を返す。
// 無効なユーザーは宛先にしない。
func (s *Service) loadActivities(ctx context.Context, scenarioID int64) ([]activityItem, map[int64]person, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.unit_id, a.code, a.name,
		       COALESCE(IF(ou.is_active, ou.id, NULL), IF(mu.is_active, mu.id, NULL), 0),
		       COALESCE(IF(ou.is_active, ou.id, NULL), 0),
		       COALESCE(IF(mu.is_active, mu.id, NULL), 0),
		       n.completed_at IS NOT NULL
		FROM activities a
		JOIN units un ON un.id = a.unit_id
		LEFT JOIN users ou ON ou.id = a.owner_user_id
		LEFT JOIN users mu ON mu.id = un.owner_user_id
		LEFT JOIN activity_scenario_notes n ON n.activity_id = a.id AND n.scenario_id = ?
		WHERE `+target.Condition("a")+`
		ORDER BY a.code`, scenarioID, scenarioID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var items []activityItem
	for rows.Next() {
		var it activityItem
		if err := rows.Scan(&it.id, &it.unitID, &it.code, &it.name, &it.assignee, &it.owner, &it.manager, &it.completed); err != nil {
			return nil, nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	people, err := s.loadPeople(ctx)
	return items, people, err
}

func (s *Service) loadPeople(ctx context.Context) (map[int64]person, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, COALESCE(slack_user_id, '') FROM users WHERE is_active")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]person{}
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.id, &p.name, &p.slackID); err != nil {
			return nil, err
		}
		out[p.id] = p
	}
	return out, rows.Err()
}

// groupBy は施策を宛先ごとにまとめる（宛先の ID の順）。
func groupBy(items []activityItem, key func(activityItem) int64) ([]int64, map[int64][]activityItem) {
	out := map[int64][]activityItem{}
	for _, it := range items {
		if k := key(it); k != 0 {
			out[k] = append(out[k], it)
		}
	}
	ids := make([]int64, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, out
}

// listActivities はお知らせの本文用に施策を並べる（10件まで）。
func listActivities(items []activityItem) string {
	var b strings.Builder
	for i, it := range items {
		if i == 10 {
			fmt.Fprintf(&b, "ほか %d 件\n", len(items)-10)
			break
		}
		fmt.Fprintf(&b, "・%s（%s）\n", it.name, it.code)
	}
	return strings.TrimRight(b.String(), "\n")
}

// mention は Slack での呼び方。メンバー ID があればメンションし、なければ名前。
func mention(p person) string {
	if p.slackID != "" {
		return "<@" + p.slackID + ">"
	}
	return p.name
}

// mentionList は「@佐藤（2件）、@鈴木（1件）」の形にする。
func mentionList(ids []int64, groups map[int64][]activityItem, people map[int64]person) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s（%d件）", mention(people[id]), len(groups[id])))
	}
	return strings.Join(parts, "、")
}

var weekdays = []string{"日", "月", "火", "水", "木", "金", "土"}

// dateLabel は「10/15（水）」の形にする。
func dateLabel(t time.Time) string {
	return fmt.Sprintf("%d/%d（%s）", int(t.Month()), t.Day(), weekdays[t.Weekday()])
}
