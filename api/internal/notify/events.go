package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// settings は通知の設定（notification_settings）。
type settings struct {
	EnabledKinds map[string]bool `json:"enabled_kinds"`
	ReminderDays []int           `json:"reminder_days"`
	SendTime     string          `json:"send_time"` // HH:MM
}

func (s *Service) loadSettings(ctx context.Context) (settings, error) {
	var st settings
	var enabled []byte
	var days, sendTime string
	err := s.db.QueryRowContext(ctx, "SELECT enabled_kinds, reminder_days, TIME_FORMAT(send_time, '%H:%i') FROM notification_settings WHERE id = 1").
		Scan(&enabled, &days, &sendTime)
	if err != nil {
		return st, err
	}
	st.EnabledKinds = map[string]bool{}
	if err := json.Unmarshal(enabled, &st.EnabledKinds); err != nil {
		return st, err
	}
	for _, k := range kinds {
		if _, ok := st.EnabledKinds[k]; !ok {
			st.EnabledKinds[k] = true
		}
	}
	st.ReminderDays = []int{}
	for _, p := range strings.Split(days, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
			st.ReminderDays = append(st.ReminderDays, n)
		}
	}
	st.SendTime = sendTime
	return st, nil
}

// UpdateStarted は「更新の開始」を送る。シナリオが作成中・締切あり・ロックされていないときだけ、シナリオごとに1回。
// 締切の保存や作成中の指定の後に呼ぶ。失敗はログに残すだけにする（保存は失敗させない）。
func (s *Service) UpdateStarted(ctx context.Context, scenarioID int64) {
	if err := s.updateStarted(ctx, scenarioID); err != nil {
		s.logger.Error("notify update_started", "scenario_id", scenarioID, "error", err)
	}
}

func (s *Service) updateStarted(ctx context.Context, scenarioID int64) error {
	st, err := s.loadSettings(ctx)
	if err != nil || !st.EnabledKinds[KindUpdateStarted] {
		return err
	}
	sc, err := s.findScenario(ctx, scenarioID)
	if err != nil {
		return err
	}
	if !sc.active || sc.locked || sc.deadline == nil {
		return nil
	}
	var sent int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_runs WHERE kind = ? AND scenario_id = ?", KindUpdateStarted, sc.id).Scan(&sent); err != nil || sent > 0 {
		return err
	}
	items, people, err := s.loadActivities(ctx, sc.id)
	if err != nil {
		return err
	}
	ids, groups := groupBy(items, func(it activityItem) int64 { return it.assignee })
	m := message{kind: KindUpdateStarted, scenarioID: sc.id, runDate: s.today()}
	title := fmt.Sprintf("見込の更新が始まりました（%s）", sc.name)
	for _, id := range ids {
		m.notes = append(m.notes, note{userID: id, title: title,
			body: fmt.Sprintf("締切は %s です。担当の施策（%d件）を更新し、今回の見込の説明を書いて「説明して完了」を押してください。\n%s", dateLabel(*sc.deadline), len(groups[id]), listActivities(groups[id])),
			link: "/"})
	}
	m.slackText = fmt.Sprintf("【%s】見込の更新が始まりました。締切は %s です。\n対象: %s%s", sc.name, dateLabel(*sc.deadline), mentionList(ids, groups, people), s.link("/", "ホームを開く"))
	return s.deliver(ctx, m)
}

// ActualsReflected は「実績の反映」を送る。months は新しく実績になった月、activityIDs は前回見込との差が大きい施策。
// 決算確定月の保存の後に呼ぶ。失敗はログに残すだけにする。
func (s *Service) ActualsReflected(ctx context.Context, scenarioID int64, months []string, activityIDs []int64) {
	if err := s.actualsReflected(ctx, scenarioID, months, activityIDs); err != nil {
		s.logger.Error("notify actuals_reflected", "scenario_id", scenarioID, "error", err)
	}
}

func (s *Service) actualsReflected(ctx context.Context, scenarioID int64, months []string, activityIDs []int64) error {
	if len(months) == 0 || len(activityIDs) == 0 {
		return nil
	}
	st, err := s.loadSettings(ctx)
	if err != nil || !st.EnabledKinds[KindActualsReflected] {
		return err
	}
	sc, err := s.findScenario(ctx, scenarioID)
	if err != nil {
		return err
	}
	if !sc.active || sc.locked {
		return nil
	}
	items, people, err := s.loadActivities(ctx, sc.id)
	if err != nil {
		return err
	}
	items = slices.DeleteFunc(items, func(it activityItem) bool { return !slices.Contains(activityIDs, it.id) })
	ids, groups := groupBy(items, func(it activityItem) int64 { return it.assignee })
	label := monthsLabel(months)
	m := message{kind: KindActualsReflected, scenarioID: sc.id, runDate: s.today()}
	for _, id := range ids {
		m.notes = append(m.notes, note{userID: id,
			title: fmt.Sprintf("%sの実績が反映されました（%s）", label, sc.name),
			body:  fmt.Sprintf("前回の見込との差が大きい施策があります（差が前回の見込の 20%% 以上）。見込を見直してください。\n%s", listActivities(groups[id])),
			link:  "/"})
	}
	m.slackText = fmt.Sprintf("【%s】%sの実績が反映されました。前回の見込との差が大きい施策: %s%s", sc.name, label, mentionList(ids, groups, people), s.link("/", "ホームを開く"))
	return s.deliver(ctx, m)
}

func monthsLabel(months []string) string {
	parts := make([]string, len(months))
	for i, m := range months {
		n, _ := strconv.Atoi(m[5:])
		parts[i] = strconv.Itoa(n) + "月"
	}
	return strings.Join(parts, "・")
}

// Run は1分ごとに RunDue を呼ぶ。ctx が終わると戻る。
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.RunDue(ctx); err != nil {
			s.logger.Error("notify run", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunDue は、送信時刻を過ぎていれば今日の「締切の前」「締切の超過」を送る（土日は送らない）。
// 送信の記録で二重送信を防ぐので、何度呼んでもよい。古いお知らせの削除もここで行う。
func (s *Service) RunDue(ctx context.Context) error {
	now := s.now().In(jst)
	st, err := s.loadSettings(ctx)
	if err != nil {
		return err
	}
	if now.Format("15:04") < st.SendTime {
		return nil
	}
	if err := s.cleanup(ctx); err != nil {
		return err
	}
	today := s.today()
	if isWeekend(today) {
		return nil
	}
	sc, err := s.activeScenario(ctx)
	if err != nil || sc == nil {
		return err
	}
	deadline := *sc.deadline
	switch {
	case today.After(deadline):
		if st.EnabledKinds[KindDeadlineOverdue] {
			return s.deadlineOverdue(ctx, *sc, today)
		}
	case st.EnabledKinds[KindDeadlineReminder]:
		for _, d := range st.ReminderDays {
			if reminderDate(deadline, d).Equal(today) {
				return s.deadlineReminder(ctx, *sc, today)
			}
		}
	}
	return nil
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// reminderDate は締切の days 日前の送信日。土日に当たる場合は直前の金曜にする。
func reminderDate(deadline time.Time, days int) time.Time {
	d := deadline.AddDate(0, 0, -days)
	for isWeekend(d) {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours()/24 + 0.5)
}

func (s *Service) deadlineReminder(ctx context.Context, sc scenarioInfo, today time.Time) error {
	items, people, err := s.loadActivities(ctx, sc.id)
	if err != nil {
		return err
	}
	items = slices.DeleteFunc(items, func(it activityItem) bool { return it.completed })
	ids, groups := groupBy(items, func(it activityItem) int64 { return it.assignee })
	left := daysBetween(today, *sc.deadline)
	when := fmt.Sprintf("あと%d日", left)
	if left == 1 {
		when = "明日"
	}
	m := message{kind: KindDeadlineReminder, scenarioID: sc.id, runDate: today}
	for _, id := range ids {
		m.notes = append(m.notes, note{userID: id,
			title: fmt.Sprintf("締切まで%sです（%s）", when, sc.name),
			body:  fmt.Sprintf("締切は %s です。まだ完了していない施策（%d件）を更新してください。\n%s", dateLabel(*sc.deadline), len(groups[id]), listActivities(groups[id])),
			link:  "/"})
	}
	m.slackText = fmt.Sprintf("【%s】見込の更新の締切まで%sです（%s）。未完了: %s%s", sc.name, when, dateLabel(*sc.deadline), mentionList(ids, groups, people), s.link("/", "ホームを開く"))
	return s.deliver(ctx, m)
}

// deadlineOverdue は締切を過ぎた未完了の施策を、担当者とユニットのマネージャーに知らせる。
// 担当者でもありマネージャーでもある人には、1件のお知らせにまとめる。
func (s *Service) deadlineOverdue(ctx context.Context, sc scenarioInfo, today time.Time) error {
	items, people, err := s.loadActivities(ctx, sc.id)
	if err != nil {
		return err
	}
	items = slices.DeleteFunc(items, func(it activityItem) bool { return it.completed })
	ownerIDs, owned := groupBy(items, func(it activityItem) int64 { return it.assignee })
	_, managed := groupBy(items, func(it activityItem) int64 { return it.manager })
	over := daysBetween(*sc.deadline, today)
	title := fmt.Sprintf("締切を%d日過ぎています（%s）", over, sc.name)

	recipients := map[int64]bool{}
	for _, id := range ownerIDs {
		recipients[id] = true
	}
	for id := range managed {
		recipients[id] = true
	}
	all := make([]int64, 0, len(recipients))
	for id := range recipients {
		all = append(all, id)
	}
	slices.Sort(all)
	m := message{kind: KindDeadlineOverdue, scenarioID: sc.id, runDate: today}
	for _, id := range all {
		var parts []string
		if len(owned[id]) > 0 {
			parts = append(parts, fmt.Sprintf("担当の未完了の施策（%d件）:\n%s", len(owned[id]), listActivities(owned[id])))
		}
		if len(managed[id]) > 0 {
			parts = append(parts, fmt.Sprintf("所管ユニットの未完了の施策（%d件）:\n%s", len(managed[id]), listActivities(managed[id])))
		}
		m.notes = append(m.notes, note{userID: id, title: title,
			body: fmt.Sprintf("締切（%s）を過ぎています。至急、更新を完了にしてください。\n%s", dateLabel(*sc.deadline), strings.Join(parts, "\n")),
			link: "/"})
	}
	managerIDs := make([]int64, 0, len(managed))
	for id := range managed {
		managerIDs = append(managerIDs, id)
	}
	slices.Sort(managerIDs)
	text := fmt.Sprintf("【%s】見込の更新の締切（%s）を%d日過ぎています。未完了: %s", sc.name, dateLabel(*sc.deadline), over, mentionList(ownerIDs, owned, people))
	if len(managerIDs) > 0 {
		names := make([]string, len(managerIDs))
		for i, id := range managerIDs {
			names[i] = mention(people[id])
		}
		text += "\nユニットのマネージャー: " + strings.Join(names, "、")
	}
	m.slackText = text + s.link("/", "ホームを開く")
	return s.deliver(ctx, m)
}
