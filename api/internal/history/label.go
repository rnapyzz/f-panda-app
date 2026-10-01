package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// labeler は監査ログの対象を人が読める名前にする。名前は現在のマスタから引き、
// 削除済みのものは記録（before/after）の値を使う。
type labeler struct {
	lines      map[int64]string
	activities map[int64]string
	subjects   map[int64]string
	drivers    map[int64]struct {
		name       string
		activityID int64
	}
}

func (h *Handler) newLabeler(ctx context.Context) (*labeler, error) {
	l := &labeler{drivers: map[int64]struct {
		name       string
		activityID int64
	}{}}
	var err error
	if l.activities, err = nameMap(ctx, h.db, "SELECT id, name FROM activities"); err != nil {
		return nil, err
	}
	if l.subjects, err = nameMap(ctx, h.db, "SELECT id, name FROM subjects"); err != nil {
		return nil, err
	}
	if l.lines, err = nameMap(ctx, h.db, "SELECT id, name FROM activity_lines"); err != nil {
		return nil, err
	}
	rows, err := h.db.QueryContext(ctx, "SELECT id, name, activity_id FROM activity_drivers")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, activityID int64
		var name string
		if err := rows.Scan(&id, &name, &activityID); err != nil {
			return nil, err
		}
		l.drivers[id] = struct {
			name       string
			activityID int64
		}{name, activityID}
	}
	return l, rows.Err()
}

var tableNouns = map[string]string{
	"scenarios":     "シナリオ",
	"units":         "ユニット",
	"functions":     "ユニット", // 改称前（functions）の監査ログ用
	"segments":      "セグメント",
	"organizations": "組織",
	"subjects":      "科目",
	"users":         "ユーザー",
}

func (l *labeler) label(log Log) string {
	rec := recordOf(log.After)
	if rec == nil {
		rec = recordOf(log.Before)
	}
	activity := func() string {
		if id, ok := rec.int("activity_id"); ok {
			return l.activityName(id)
		}
		return ""
	}
	switch log.TableName {
	case "activities":
		if name, ok := l.activities[log.RecordID]; ok {
			return "施策 " + name
		}
		return "施策 " + rec.str("name")
	case "activity_external_codes":
		return activity() + " / 外部コード「" + rec.str("code") + "」"
	case "activity_milestones":
		return activity() + " / マイルストーン「" + rec.str("name") + "」"
	case "activity_drivers":
		return activity() + " / ドライバー「" + rec.str("name") + "」"
	case "activity_lines":
		return activity() + " / " + l.subjectName(rec) + " / 内訳「" + rec.str("name") + "」"
	case "activity_formulas": // 内訳に置き換える前の計算式（改称前の監査ログ用）
		return activity() + " / 計算式（" + l.subjectName(rec) + "）"
	case "budget_facts":
		if id, ok := rec.int("line_id"); ok {
			return activity() + " / " + l.subjectName(rec) + " / " + l.lineName(id) + " / " + rec.str("target_month")
		}
		return activity() + " / " + l.subjectName(rec) + " / " + rec.str("target_month")
	case "actual_facts":
		return activity() + " / " + l.subjectName(rec) + " / " + rec.str("target_month") + "（実績）"
	case "driver_values":
		id, _ := rec.int("activity_driver_id")
		if d, ok := l.drivers[id]; ok {
			return l.activityName(d.activityID) + " / " + d.name + " / " + rec.str("target_month")
		}
		return fmt.Sprintf("（削除されたドライバー #%d） / %s", id, rec.str("target_month"))
	case "scenario_conditions":
		return activity() + " / 想定条件"
	}
	if noun, ok := tableNouns[log.TableName]; ok {
		if name := rec.str("name"); name != "" {
			return noun + " " + name
		}
		if log.TableName == "users" {
			return fmt.Sprintf("%s #%d（パスワード）", noun, log.RecordID)
		}
		return fmt.Sprintf("%s #%d", noun, log.RecordID)
	}
	return fmt.Sprintf("%s #%d", log.TableName, log.RecordID)
}

func (l *labeler) activityName(id int64) string {
	if name, ok := l.activities[id]; ok {
		return name
	}
	return fmt.Sprintf("（削除された施策 #%d）", id)
}

func (l *labeler) lineName(id int64) string {
	if name, ok := l.lines[id]; ok {
		return "内訳「" + name + "」"
	}
	return fmt.Sprintf("（削除された内訳 #%d）", id)
}

func (l *labeler) subjectName(rec record) string {
	id, _ := rec.int("subject_id")
	if name, ok := l.subjects[id]; ok {
		return name
	}
	return fmt.Sprintf("科目 #%d", id)
}

// record は監査ログの before/after の JSON。
type record map[string]any

func recordOf(raw json.RawMessage) record {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func (r record) str(key string) string {
	s, _ := r[key].(string)
	return s
}

func (r record) int(key string) (int64, bool) {
	f, ok := r[key].(float64)
	return int64(f), ok
}

func nameMap(ctx context.Context, db *sql.DB, query string) (map[int64]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
