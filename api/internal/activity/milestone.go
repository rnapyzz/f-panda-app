package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

var milestoneStatuses = []string{"not_started", "in_progress", "completed", "delayed"}

// milestone は施策のマイルストーン。
type milestone struct {
	ID         int64  `json:"id"`
	ActivityID int64  `json:"activity_id"`
	Name       string `json:"name"`
	DueDate    string `json:"due_date"`
	Status     string `json:"status"`
	timestamps
}

type milestoneRequest struct {
	Name    string `json:"name"`
	DueDate string `json:"due_date"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
}

const milestoneSelect = "SELECT id, activity_id, name, due_date, status, created_at, updated_at FROM activity_milestones"

func scanMilestone(row interface{ Scan(...any) error }) (milestone, error) {
	var m milestone
	var due sql.NullTime
	err := row.Scan(&m.ID, &m.ActivityID, &m.Name, &due, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if d := formatDate(due); d != nil {
		m.DueDate = *d
	}
	return m, err
}

func listMilestones(ctx context.Context, q querier, activityID int64) ([]milestone, error) {
	rows, err := q.QueryContext(ctx, milestoneSelect+" WHERE activity_id = ? ORDER BY due_date, id", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []milestone{}
	for rows.Next() {
		m, err := scanMilestone(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func findMilestone(ctx context.Context, tx *sql.Tx, activityID, id int64) (milestone, error) {
	m, err := scanMilestone(tx.QueryRowContext(ctx, milestoneSelect+" WHERE id = ? AND activity_id = ? FOR UPDATE", id, activityID))
	if errors.Is(err, sql.ErrNoRows) {
		return milestone{}, httpx.NotFound("マイルストーンが見つかりません")
	}
	return m, err
}

func validateMilestone(req milestoneRequest) (name, due string, err error) {
	v := httpx.Validator{}
	name = v.Text("name", "マイルストーン名", req.Name, maxNameLen)
	d, ok := parseDate(v, "due_date", "期日", &req.DueDate)
	if ok && !d.Valid {
		v.Add("due_date", "期日を入力してください")
	}
	if !slices.Contains(milestoneStatuses, req.Status) {
		v.Add("status", "ステータスは "+strings.Join(milestoneStatuses, " / ")+" のいずれかを指定してください")
	}
	return name, d.String, v.Err()
}

// createMilestone は POST /api/activities/{id}/milestones。
func (h *Handler) createMilestone(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req milestoneRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.Status == "" {
		req.Status = "not_started"
	}
	name, due, err := validateMilestone(req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created milestone
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO activity_milestones (activity_id, name, due_date, status) VALUES (?, ?, ?, ?)",
			activityID, name, due, req.Status,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findMilestone(ctx, tx, activityID, id); err != nil {
			return err
		}
		return rec.Insert(ctx, "activity_milestones", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateMilestone は PUT /api/activities/{id}/milestones/{mid}。期日を変更する場合は変更理由が必須。
func (h *Handler) updateMilestone(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "mid")
	if err != nil {
		return err
	}
	var req milestoneRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, due, err := validateMilestone(req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated milestone
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findMilestone(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		if before.DueDate != due {
			if err := requireReason(req.Reason); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE activity_milestones SET name = ?, due_date = ?, status = ? WHERE id = ?",
			name, due, req.Status, id,
		); err != nil {
			return err
		}
		if updated, err = findMilestone(ctx, tx, activityID, id); err != nil {
			return err
		}
		return rec.Update(ctx, "activity_milestones", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteMilestone は DELETE /api/activities/{id}/milestones/{mid}。変更理由が必須。
func (h *Handler) deleteMilestone(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "mid")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := requireReason(req.Reason); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findMilestone(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity_milestones WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "activity_milestones", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
