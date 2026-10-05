package scenario

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/activity"
	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 施策 × シナリオの差異の説明と更新の完了（docs/plan.md「2.10 現場担当の動線」）。
//
// 状態は、完了にしていれば「完了」、数値や説明を変更していれば「入力中」、どちらもなければ「未着手」。
// 数値（金額・ドライバー値・想定条件）や説明を変更すると、変更日時を記録して完了を取り消す（入力中に戻す）。

// noteCauses は要因の分類。
var noteCauses = []string{"timing", "volume", "new", "lost", "assumption", "other"}

const (
	statusNotStarted = "not_started"
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
)

// note は施策 × シナリオの差異の説明と状態。
type note struct {
	ID              int64      `json:"-"`
	ScenarioID      int64      `json:"scenario_id"`
	ActivityID      int64      `json:"activity_id"`
	Explanation     string     `json:"explanation"`
	Causes          []string   `json:"causes"`
	Status          string     `json:"status"` // not_started / in_progress / completed
	CompletedAt     *time.Time `json:"completed_at"`
	CompletedBy     *int64     `json:"completed_by"`
	CompletedByName string     `json:"completed_by_name"`
	LastEditedAt    *time.Time `json:"last_edited_at"`
}

// loadNote は施策 × シナリオの記録を読む。なければ未着手の空の記録を返す。lock は " FOR UPDATE" など。
func loadNote(ctx context.Context, q queryer, scenarioID, activityID int64, lock string) (note, bool, error) {
	n := note{ScenarioID: scenarioID, ActivityID: activityID, Causes: []string{}}
	var explanation, completedByName sql.NullString
	var causes string
	var completedAt, editedAt sql.NullTime
	var completedBy sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT n.id, n.explanation, n.causes, n.completed_at, n.completed_by, u.name, n.last_edited_at
		FROM activity_scenario_notes n LEFT JOIN users u ON u.id = n.completed_by
		WHERE n.scenario_id = ? AND n.activity_id = ?`+lock, scenarioID, activityID,
	).Scan(&n.ID, &explanation, &causes, &completedAt, &completedBy, &completedByName, &editedAt)
	if errors.Is(err, sql.ErrNoRows) {
		n.Status = statusNotStarted
		return n, false, nil
	}
	if err != nil {
		return n, false, err
	}
	n.Explanation = explanation.String
	if causes != "" {
		n.Causes = strings.Split(causes, ",")
	}
	if completedAt.Valid {
		n.CompletedAt = &completedAt.Time
	}
	n.CompletedBy = dbx.PtrInt64(completedBy)
	n.CompletedByName = completedByName.String
	if editedAt.Valid {
		n.LastEditedAt = &editedAt.Time
	}
	switch {
	case n.CompletedAt != nil:
		n.Status = statusCompleted
	case n.LastEditedAt != nil:
		n.Status = statusInProgress
	default:
		n.Status = statusNotStarted
	}
	return n, true, nil
}

// noteRecord は監査ログに残す記録（表示用の名前・状態は含めない）。
type noteRecord struct {
	ScenarioID  int64      `json:"scenario_id"`
	ActivityID  int64      `json:"activity_id"`
	Explanation string     `json:"explanation"`
	Causes      []string   `json:"causes"`
	CompletedAt *time.Time `json:"completed_at"`
	CompletedBy *int64     `json:"completed_by"`
}

func recordOf(n note) noteRecord {
	return noteRecord{ScenarioID: n.ScenarioID, ActivityID: n.ActivityID, Explanation: n.Explanation, Causes: n.Causes, CompletedAt: n.CompletedAt, CompletedBy: n.CompletedBy}
}

// markEdited は施策 × シナリオを変更したことを記録し、完了していれば取り消す（入力中に戻す）。
// 数値・想定条件・説明を変更したトランザクションの中で呼ぶ。
func markEdited(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, scenarioID, activityID int64) error {
	before, found, err := loadNote(ctx, tx, scenarioID, activityID, " FOR UPDATE")
	if err != nil {
		return err
	}
	if !found {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO activity_scenario_notes (scenario_id, activity_id, last_edited_at) VALUES (?, ?, NOW())", scenarioID, activityID)
		if err != nil {
			return err
		}
		_, err = res.LastInsertId()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE activity_scenario_notes SET last_edited_at = NOW(), completed_at = NULL, completed_by = NULL WHERE id = ?", before.ID); err != nil {
		return err
	}
	if before.CompletedAt == nil {
		return nil
	}
	// 完了の取り消しは、変更履歴に残す
	after := recordOf(before)
	after.CompletedAt, after.CompletedBy = nil, nil
	return rec.Update(ctx, "activity_scenario_notes", before.ID, recordOf(before), after)
}

// putNote は PUT /api/scenarios/{id}/activities/{aid}/note。差異の説明と要因の分類を保存する。
// 権限は数値の入力と同じ。変更すると入力中に戻る。
func (h *Handler) putNote(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	var req struct {
		Explanation string   `json:"explanation"`
		Causes      []string `json:"causes"`
		Reason      string   `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	explanation := v.OptionalText("explanation", "差異の説明", req.Explanation, 5000)
	causes := []string{}
	for _, c := range noteCauses { // 定義の順にそろえる
		if slices.Contains(req.Causes, c) {
			causes = append(causes, c)
		}
	}
	for _, c := range req.Causes {
		if !slices.Contains(noteCauses, c) {
			v.Add("causes", "要因の分類は "+strings.Join(noteCauses, " / ")+" から選んでください")
			break
		}
	}
	if err := v.Err(); err != nil {
		return err
	}

	err = h.editTxRaw(r, scenarioID, activityID, req.Reason, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error {
		before, found, err := loadNote(ctx, tx, scenarioID, activityID, " FOR UPDATE")
		if err != nil {
			return err
		}
		if found && before.Explanation == explanation && slices.Equal(before.Causes, causes) {
			return nil
		}
		if err := markEdited(ctx, tx, rec, scenarioID, activityID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE activity_scenario_notes SET explanation = ?, causes = ? WHERE scenario_id = ? AND activity_id = ?",
			dbx.NullString(explanation), strings.Join(causes, ","), scenarioID, activityID); err != nil {
			return err
		}
		after, _, err := loadNote(ctx, tx, scenarioID, activityID, "")
		if err != nil {
			return err
		}
		b := recordOf(before)
		b.CompletedAt, b.CompletedBy = nil, nil // 完了の取り消しは markEdited で記録済み
		return rec.Update(ctx, "activity_scenario_notes", after.ID, b, recordOf(after))
	})
	if err != nil {
		return err
	}
	return h.respondValues(w, r, scenarioID, activityID)
}

// complete は POST /api/scenarios/{id}/activities/{aid}/complete。更新を完了にする（承認ではなく状態の記録）。
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) error {
	return h.setCompleted(w, r, true)
}

// uncomplete は DELETE /api/scenarios/{id}/activities/{aid}/complete。完了を取り消して入力中に戻す。
func (h *Handler) uncomplete(w http.ResponseWriter, r *http.Request) error {
	return h.setCompleted(w, r, false)
}

func (h *Handler) setCompleted(w http.ResponseWriter, r *http.Request, completed bool) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	err = h.editTxRaw(r, scenarioID, activityID, req.Reason, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, s Scenario, a activity.Summary) error {
		before, found, err := loadNote(ctx, tx, scenarioID, activityID, " FOR UPDATE")
		if err != nil {
			return err
		}
		if (before.CompletedAt != nil) == completed {
			return nil // すでにその状態
		}
		if completed {
			if !found {
				// 何も変更せずに完了にする（見込を変えなかった）場合
				if _, err := tx.ExecContext(ctx,
					"INSERT INTO activity_scenario_notes (scenario_id, activity_id, last_edited_at) VALUES (?, ?, NOW())", scenarioID, activityID); err != nil {
					return err
				}
				if before, _, err = loadNote(ctx, tx, scenarioID, activityID, " FOR UPDATE"); err != nil {
					return err
				}
			}
			_, err = tx.ExecContext(ctx, "UPDATE activity_scenario_notes SET completed_at = NOW(), completed_by = ? WHERE id = ?", u.ID, before.ID)
		} else {
			_, err = tx.ExecContext(ctx, "UPDATE activity_scenario_notes SET completed_at = NULL, completed_by = NULL WHERE id = ?", before.ID)
		}
		if err != nil {
			return err
		}
		after, _, err := loadNote(ctx, tx, scenarioID, activityID, "")
		if err != nil {
			return err
		}
		return rec.Update(ctx, "activity_scenario_notes", after.ID, recordOf(before), recordOf(after))
	})
	if err != nil {
		return err
	}
	return h.respondValues(w, r, scenarioID, activityID)
}

// getNote は GET /api/scenarios/{id}/activities/{aid}/note。
func (h *Handler) getNote(w http.ResponseWriter, r *http.Request) error {
	scenarioID, activityID, err := pathIDs(r)
	if err != nil {
		return err
	}
	if _, err := findScenario(r.Context(), h.db, scenarioID, ""); err != nil {
		return err
	}
	n, _, err := loadNote(r.Context(), h.db, scenarioID, activityID, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, n)
	return nil
}
