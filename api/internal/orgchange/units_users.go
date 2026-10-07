package orgchange

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// --- ユニットの統合と廃止 ---

type mergeRequest struct {
	TargetUnitID int64  `json:"target_unit_id"`
	Reason       string `json:"reason"`
}

// mergeUnit は POST /api/units/{id}/merge。施策をすべて統合先へ移し、ユニットを廃止にする。理由は必須。
func (s *Service) mergeUnit(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req mergeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	if req.TargetUnitID == 0 {
		v.Add("target_unit_id", "統合先のユニットを選んでください")
	}
	if strings.TrimSpace(req.Reason) == "" {
		v.Add("reason", "ユニットの統合には変更理由の入力が必要です")
	}
	if err := v.Err(); err != nil {
		return err
	}
	ctx := r.Context()
	var moved int
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		var err error
		moved, err = MergeUnit(ctx, tx, rec, id, req.TargetUnitID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"moved": moved})
	return nil
}

// archiveUnit は POST /api/units/{id}/archive。施策が所属しているユニットは廃止できない（統合を使う）。
func (s *Service) archiveUnit(w http.ResponseWriter, r *http.Request) error {
	return s.setArchived(w, r, true)
}

// unarchiveUnit は POST /api/units/{id}/unarchive。廃止を取り消す。
func (s *Service) unarchiveUnit(w http.ResponseWriter, r *http.Request) error {
	return s.setArchived(w, r, false)
}

func (s *Service) setArchived(w http.ResponseWriter, r *http.Request, archived bool) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findUnit(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.IsArchived == archived {
			return nil
		}
		if archived {
			n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activities WHERE unit_id = ?", id)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.Conflict("施策が所属しているユニットは廃止できません。「統合」で施策をほかのユニットへ移してください")
			}
		}
		return setArchived(ctx, tx, rec, before, archived)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// --- 無効化のときの後任者 ---

type assignmentCounts struct {
	Activities int `json:"activities"` // 担当している施策
	Units      int `json:"units"`      // 所管ユニット（廃止を除く）
}

func countAssignments(ctx context.Context, q querier, userID int64) (assignmentCounts, error) {
	var c assignmentCounts
	err := q.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM activities WHERE owner_user_id = ?),
		       (SELECT COUNT(*) FROM units WHERE owner_user_id = ? AND NOT is_archived)`, userID, userID).Scan(&c.Activities, &c.Units)
	return c, err
}

// assignments は GET /api/users/{id}/assignments。
func (s *Service) assignments(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	c, err := countAssignments(r.Context(), s.db, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, c)
	return nil
}

type deactivateRequest struct {
	// SuccessorUserID は後任者。null なら担当者・マネージャーを未設定にする
	SuccessorUserID *int64 `json:"successor_user_id"`
	Reason          string `json:"reason"`
}

// userRow は監査ログに残すユーザー（パスワードハッシュは含めない）。
type userRow struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
}

// deactivate は POST /api/users/{id}/deactivate。担当している施策と所管ユニットを後任者へ付け替えてから、ユーザーを無効にする。
// 自分自身は無効にできない。
func (s *Service) deactivate(w http.ResponseWriter, r *http.Request) error {
	me, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req deactivateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if id == me.ID {
		return httpx.Validation(map[string]string{"is_active": "自分自身は無効化できません"})
	}
	if req.SuccessorUserID != nil && *req.SuccessorUserID == id {
		return httpx.Validation(map[string]string{"successor_user_id": "後任者に本人は選べません"})
	}
	ctx := r.Context()
	var moved assignmentCounts
	err = audit.InTx(ctx, s.db, me.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		var before userRow
		err := tx.QueryRowContext(ctx, "SELECT id, name, email, role, is_active FROM users WHERE id = ? FOR UPDATE", id).
			Scan(&before.ID, &before.Name, &before.Email, &before.Role, &before.IsActive)
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.NotFound("ユーザーが見つかりません")
		}
		if err != nil {
			return err
		}
		if err := checkActiveUser(ctx, tx, req.SuccessorUserID); err != nil {
			return httpx.Validation(map[string]string{"successor_user_id": message(err)})
		}
		activityIDs, err := idList(ctx, tx, "SELECT id FROM activities WHERE owner_user_id = ? ORDER BY id FOR UPDATE", id)
		if err != nil {
			return err
		}
		for _, aid := range activityIDs {
			if err := changeActivityOwner(ctx, tx, rec, aid, req.SuccessorUserID); err != nil {
				return err
			}
		}
		unitIDs, err := idList(ctx, tx, "SELECT id FROM units WHERE owner_user_id = ? ORDER BY id FOR UPDATE", id)
		if err != nil {
			return err
		}
		for _, uid := range unitIDs {
			if err := changeUnitOwner(ctx, tx, rec, uid, req.SuccessorUserID); err != nil {
				return err
			}
		}
		moved = assignmentCounts{Activities: len(activityIDs), Units: len(unitIDs)}
		if !before.IsActive {
			return nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE users SET is_active = FALSE WHERE id = ?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id); err != nil {
			return err
		}
		after := before
		after.IsActive = false
		return rec.Update(ctx, "users", id, before, after)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"moved": moved})
	return nil
}

func idList(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
