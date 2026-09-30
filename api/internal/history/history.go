// Package history は変更履歴（変更セットと監査ログ）を参照する API を提供する。
//
// 参照はログインユーザー全員（数値の根拠と変更理由を誰でも確認できるようにするため）。
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	// 一覧に表示する、変更セットごとの施策の数の上限
	maxActivitiesPerSet = 5
)

// Handler は変更履歴 API のハンドラー。
type Handler struct {
	db *sql.DB
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Register はルートを登録する。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/change-sets", requireAuth(httpx.Handle(h.list)))
	mux.Handle("GET /api/change-sets/{id}", requireAuth(httpx.Handle(h.get)))
}

type ref struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type activityRef struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// ChangeSet は変更セットの一覧の1行。
type ChangeSet struct {
	ID         int64          `json:"id"`
	CreatedAt  time.Time      `json:"created_at"`
	User       ref            `json:"user"`
	Scenario   *ref           `json:"scenario"`
	Reason     string         `json:"reason"`
	Changes    int            `json:"changes"`
	Tables     map[string]int `json:"tables"`
	Activities []activityRef  `json:"activities"`
	// MoreActivities は Activities に含めきれなかった施策の数
	MoreActivities int `json:"more_activities"`
}

// activityTables は、記録に activity_id を持つテーブル。
var activityTables = []string{"activity_milestones", "activity_drivers", "activity_formulas", "budget_facts", "scenario_conditions"}

// activityFilter は、監査ログ a が施策 ? に関係するかを判定する SQL 条件。引数を3つ取る。
const activityFilter = `(
	(a.table_name = 'activities' AND a.record_id = ?)
	OR (a.table_name IN ('activity_milestones', 'activity_drivers', 'activity_formulas', 'budget_facts', 'scenario_conditions')
	    AND CAST(COALESCE(JSON_EXTRACT(a.after_json, '$.activity_id'), JSON_EXTRACT(a.before_json, '$.activity_id')) AS UNSIGNED) = ?)
	OR (a.table_name = 'driver_values'
	    AND CAST(COALESCE(JSON_EXTRACT(a.after_json, '$.activity_driver_id'), JSON_EXTRACT(a.before_json, '$.activity_driver_id')) AS UNSIGNED)
	        IN (SELECT id FROM activity_drivers WHERE activity_id = ?))
)`

// list は GET /api/change-sets。新しい順に返す。
//
// クエリパラメーター:
//   - scenario_id / activity_id / user_id: 絞り込み
//   - reason: with（理由あり）/ without（理由なし）
//   - from / to: 期間（YYYY-MM-DD、to の日を含む）
//   - before_id: この ID より古いものを返す（続きの取得）
//   - limit: 件数（既定50、最大200）
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()

	var where []string
	var args []any
	for _, p := range []struct{ param, column string }{{"scenario_id", "c.scenario_id"}, {"user_id", "c.user_id"}, {"before_id", "c.id"}} {
		s := q.Get(p.param)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest(p.param + " は数値で指定してください")
		}
		op := " = ?"
		if p.param == "before_id" {
			op = " < ?"
		}
		where = append(where, p.column+op)
		args = append(args, id)
	}
	if s := q.Get("activity_id"); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return httpx.BadRequest("activity_id は数値で指定してください")
		}
		where = append(where, "EXISTS (SELECT 1 FROM audit_logs a WHERE a.change_set_id = c.id AND "+activityFilter+")")
		args = append(args, id, id, id)
	}
	switch q.Get("reason") {
	case "":
	case "with":
		where = append(where, "c.reason IS NOT NULL AND c.reason <> ''")
	case "without":
		where = append(where, "(c.reason IS NULL OR c.reason = '')")
	default:
		return httpx.BadRequest("reason は with または without を指定してください")
	}
	for _, p := range []struct{ param, op string }{{"from", ">="}, {"to", "<"}} {
		s := q.Get(p.param)
		if s == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			return httpx.BadRequest(p.param + " は YYYY-MM-DD で指定してください")
		}
		if p.param == "to" {
			d = d.AddDate(0, 0, 1) // to の日を含める
		}
		where = append(where, "c.created_at "+p.op+" ?")
		args = append(args, d.Format("2006-01-02"))
	}
	limit := defaultLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return httpx.BadRequest("limit は正の整数で指定してください")
		}
		limit = min(n, maxLimit)
	}

	query := `
		SELECT c.id, c.created_at, u.id, u.name, s.id, s.name, COALESCE(c.reason, ''),
		       (SELECT COUNT(*) FROM audit_logs a WHERE a.change_set_id = c.id)
		FROM change_sets c
		JOIN users u ON u.id = c.user_id
		LEFT JOIN scenarios s ON s.id = c.scenario_id`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	// 監査ログが1件もない変更セット（変更がなかった操作）は表示しない
	if len(where) > 0 {
		query += " AND"
	} else {
		query += " WHERE"
	}
	query += " EXISTS (SELECT 1 FROM audit_logs a WHERE a.change_set_id = c.id) ORDER BY c.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := h.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []ChangeSet
	for rows.Next() {
		var cs ChangeSet
		var sid sql.NullInt64
		var sname sql.NullString
		if err := rows.Scan(&cs.ID, &cs.CreatedAt, &cs.User.ID, &cs.User.Name, &sid, &sname, &cs.Reason, &cs.Changes); err != nil {
			return err
		}
		if sid.Valid {
			cs.Scenario = &ref{ID: sid.Int64, Name: sname.String}
		}
		items = append(items, cs)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	if err := h.fillSummaries(ctx, items); err != nil {
		return err
	}
	if items == nil {
		items = []ChangeSet{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": hasMore})
	return nil
}

// fillSummaries は変更セットごとに、テーブル別の件数と関係する施策を埋める。
func (h *Handler) fillSummaries(ctx context.Context, items []ChangeSet) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]any, len(items))
	index := map[int64]*ChangeSet{}
	for i := range items {
		ids[i] = items[i].ID
		items[i].Tables = map[string]int{}
		items[i].Activities = []activityRef{}
		index[items[i].ID] = &items[i]
	}
	rows, err := h.db.QueryContext(ctx, `
		SELECT a.change_set_id, a.table_name, a.record_id,
		       CAST(COALESCE(JSON_EXTRACT(a.after_json, '$.activity_id'), JSON_EXTRACT(a.before_json, '$.activity_id')) AS UNSIGNED),
		       CAST(COALESCE(JSON_EXTRACT(a.after_json, '$.activity_driver_id'), JSON_EXTRACT(a.before_json, '$.activity_driver_id')) AS UNSIGNED)
		FROM audit_logs a WHERE a.change_set_id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	activitySets := map[int64]map[int64]bool{}
	driverSets := map[int64]map[int64]bool{}
	for rows.Next() {
		var csID, recordID int64
		var table string
		var activityID, driverID sql.NullInt64
		if err := rows.Scan(&csID, &table, &recordID, &activityID, &driverID); err != nil {
			return err
		}
		cs := index[csID]
		cs.Tables[table]++
		switch {
		case table == "activities":
			addTo(activitySets, csID, recordID)
		case table == "driver_values" && driverID.Valid:
			addTo(driverSets, csID, driverID.Int64)
		case slices.Contains(activityTables, table) && activityID.Valid:
			addTo(activitySets, csID, activityID.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// ドライバー → 施策
	driverActivity, err := h.idMap(ctx, "SELECT id, activity_id FROM activity_drivers")
	if err != nil {
		return err
	}
	for csID, drivers := range driverSets {
		for d := range drivers {
			if a, ok := driverActivity[d]; ok {
				addTo(activitySets, csID, a)
			}
		}
	}

	activities, err := h.activityRefs(ctx)
	if err != nil {
		return err
	}
	for csID, set := range activitySets {
		cs := index[csID]
		var list []activityRef
		for id := range set {
			if a, ok := activities[id]; ok {
				list = append(list, a)
			} else {
				list = append(list, activityRef{ID: id, Code: "", Name: fmt.Sprintf("（削除された施策 #%d）", id)})
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Code < list[j].Code })
		if len(list) > maxActivitiesPerSet {
			cs.MoreActivities = len(list) - maxActivitiesPerSet
			list = list[:maxActivitiesPerSet]
		}
		cs.Activities = list
	}
	return nil
}

// Log は監査ログの1件。Label は対象のレコードを人が読める形で表したもの。
type Log struct {
	ID        int64           `json:"id"`
	TableName string          `json:"table_name"`
	RecordID  int64           `json:"record_id"`
	Action    string          `json:"action"`
	Label     string          `json:"label"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
}

// get は GET /api/change-sets/{id}。変更セットと、含まれる監査ログをすべて返す。
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var cs ChangeSet
	var sid sql.NullInt64
	var sname sql.NullString
	err = h.db.QueryRowContext(ctx, `
		SELECT c.id, c.created_at, u.id, u.name, s.id, s.name, COALESCE(c.reason, '')
		FROM change_sets c JOIN users u ON u.id = c.user_id LEFT JOIN scenarios s ON s.id = c.scenario_id
		WHERE c.id = ?`, id).Scan(&cs.ID, &cs.CreatedAt, &cs.User.ID, &cs.User.Name, &sid, &sname, &cs.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return httpx.NotFound("変更履歴が見つかりません")
	}
	if err != nil {
		return err
	}
	if sid.Valid {
		cs.Scenario = &ref{ID: sid.Int64, Name: sname.String}
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, table_name, record_id, action, COALESCE(before_json, 'null'), COALESCE(after_json, 'null')
		FROM audit_logs WHERE change_set_id = ? ORDER BY id`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var l Log
		var before, after string
		if err := rows.Scan(&l.ID, &l.TableName, &l.RecordID, &l.Action, &before, &after); err != nil {
			return err
		}
		l.Before, l.After = json.RawMessage(before), json.RawMessage(after)
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	cs.Changes = len(logs)

	labeler, err := h.newLabeler(ctx)
	if err != nil {
		return err
	}
	for i := range logs {
		logs[i].Label = labeler.label(logs[i])
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"change_set": cs, "logs": logs})
	return nil
}

// --- 補助 ---

func (h *Handler) idMap(ctx context.Context, query string) (map[int64]int64, error) {
	rows, err := h.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var k, v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (h *Handler) activityRefs(ctx context.Context) (map[int64]activityRef, error) {
	rows, err := h.db.QueryContext(ctx, "SELECT id, code, name FROM activities")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]activityRef{}
	for rows.Next() {
		var a activityRef
		if err := rows.Scan(&a.ID, &a.Code, &a.Name); err != nil {
			return nil, err
		}
		out[a.ID] = a
	}
	return out, rows.Err()
}

func addTo(m map[int64]map[int64]bool, k, v int64) {
	if m[k] == nil {
		m[k] = map[int64]bool{}
	}
	m[k][v] = true
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
