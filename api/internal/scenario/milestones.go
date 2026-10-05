package scenario

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// ホームのマイルストーン（docs/plan.md「2.11 マネージャーの動線」の ③）。

const (
	// upcomingDays は「期日が近い」とみなす日数
	upcomingDays = 30
	// maxMilestoneActivities は1回に指定できる施策の数
	maxMilestoneActivities = 200
)

// postponement は期日の後ろ倒し（前回見込の作成以降）。
type postponement struct {
	Count int `json:"count"`
	Days  int `json:"days"`
}

// homeMilestone は完了していないマイルストーンと注意。
type homeMilestone struct {
	ID         int64        `json:"id"`
	ActivityID int64        `json:"activity_id"`
	Name       string       `json:"name"`
	DueDate    string       `json:"due_date"`
	Status     string       `json:"status"`
	Overdue    bool         `json:"overdue"`  // 期日超過
	Delayed    bool         `json:"delayed"`  // 状態が遅延
	Upcoming   bool         `json:"upcoming"` // 30日以内に期日
	Postponed  postponement `json:"postponed"`
}

type homeMilestonesResponse struct {
	Today string `json:"today"`
	// Since は後ろ倒しを数え始める日時（前回見込の作成日時。前回見込がなければシナリオの作成日時）
	Since time.Time       `json:"since"`
	Items []homeMilestone `json:"items"`
}

// milestones は GET /api/scenarios/{id}/milestones?activity_ids=1,2,3。
// 指定した施策の、完了していないマイルストーンを期日の順に返す。期日超過・遅延・期日が近い・後ろ倒しの注意を付ける。
// 後ろ倒しは、監査ログのうち期日を後ろにずらした変更を、前回見込の作成以降について数える。
func (h *Handler) milestones(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var ids []int64
	for _, p := range strings.Split(r.URL.Query().Get("activity_ids"), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return httpx.BadRequest("activity_ids は施策 ID をカンマ区切りで指定してください")
		}
		ids = append(ids, n)
	}
	if len(ids) > maxMilestoneActivities {
		return httpx.BadRequest("activity_ids は " + strconv.Itoa(maxMilestoneActivities) + " 件までです")
	}

	ctx := r.Context()
	s, err := findScenario(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	since := s.CreatedAt
	if s.PreviousScenarioID != nil {
		p, err := findScenario(ctx, h.db, *s.PreviousScenarioID, "")
		if err != nil {
			return err
		}
		since = p.CreatedAt
	}
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		jst = time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	today := time.Now().In(jst)
	resp := homeMilestonesResponse{Today: today.Format("2006-01-02"), Since: since, Items: []homeMilestone{}}
	if len(ids) == 0 {
		httpx.WriteJSON(w, http.StatusOK, resp)
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, since)
	for _, id := range ids {
		args = append(args, id)
	}
	// 後ろ倒し: 期日を後ろにずらした更新の回数と、ずらした日数の合計
	rows, err := h.db.QueryContext(ctx, `
		SELECT m.id, m.activity_id, m.name, DATE_FORMAT(m.due_date, '%Y-%m-%d'), m.status,
		       COUNT(p.id), COALESCE(SUM(p.days), 0)
		FROM activity_milestones m
		LEFT JOIN (
			SELECT a.id, a.record_id,
			       DATEDIFF(JSON_UNQUOTE(JSON_EXTRACT(a.after_json, '$.due_date')), JSON_UNQUOTE(JSON_EXTRACT(a.before_json, '$.due_date'))) AS days
			FROM audit_logs a
			WHERE a.table_name = 'activity_milestones' AND a.action = 'update' AND a.created_at >= ?
			  AND JSON_UNQUOTE(JSON_EXTRACT(a.after_json, '$.due_date')) > JSON_UNQUOTE(JSON_EXTRACT(a.before_json, '$.due_date'))
		) p ON p.record_id = m.id
		WHERE m.status <> 'completed' AND m.activity_id IN (`+placeholders+`)
		GROUP BY m.id, m.activity_id, m.name, m.due_date, m.status
		ORDER BY m.due_date, m.id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	soon := today.AddDate(0, 0, upcomingDays).Format("2006-01-02")
	for rows.Next() {
		var m homeMilestone
		if err := rows.Scan(&m.ID, &m.ActivityID, &m.Name, &m.DueDate, &m.Status, &m.Postponed.Count, &m.Postponed.Days); err != nil {
			return err
		}
		m.Overdue = m.DueDate < resp.Today
		m.Delayed = m.Status == "delayed"
		m.Upcoming = !m.Overdue && m.DueDate <= soon
		resp.Items = append(resp.Items, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}
