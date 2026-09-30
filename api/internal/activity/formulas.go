package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/formula"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const maxExpressionLen = 1000

// formulaItem は施策×科目の計算式。
type formulaItem struct {
	ID         int64  `json:"id"`
	ActivityID int64  `json:"activity_id"`
	SubjectID  int64  `json:"subject_id"`
	Expression string `json:"expression"`
	timestamps
}

type formulaRequest struct {
	Expression string `json:"expression"`
	Reason     string `json:"reason"`
}

const formulaSelect = "SELECT id, activity_id, subject_id, expression, created_at, updated_at FROM activity_formulas"

func scanFormula(row interface{ Scan(...any) error }) (formulaItem, error) {
	var f formulaItem
	err := row.Scan(&f.ID, &f.ActivityID, &f.SubjectID, &f.Expression, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

func listFormulas(ctx context.Context, q querier, activityID int64) ([]formulaItem, error) {
	rows, err := q.QueryContext(ctx, formulaSelect+" WHERE activity_id = ? ORDER BY subject_id", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []formulaItem{}
	for rows.Next() {
		f, err := scanFormula(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, f)
	}
	return items, rows.Err()
}

func findFormula(ctx context.Context, tx *sql.Tx, activityID, subjectID int64) (formulaItem, bool, error) {
	f, err := scanFormula(tx.QueryRowContext(ctx, formulaSelect+" WHERE activity_id = ? AND subject_id = ? FOR UPDATE", activityID, subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return formulaItem{}, false, nil
	}
	return f, err == nil, err
}

// putFormula は PUT /api/activities/{id}/formulas/{subject_id}。科目の計算式を登録・更新する。
// 式に使える識別子は、施策のドライバーの code と probability（施策の確度）。変更理由が必須。
func (h *Handler) putFormula(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	subjectID, err := httpx.PathID(r, "subject_id")
	if err != nil {
		return err
	}
	var req formulaRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	expr := v.Text("expression", "計算式", req.Expression, maxExpressionLen)
	if err := v.Err(); err != nil {
		return err
	}
	parsed, err := formula.Parse(expr)
	if err != nil {
		return httpx.Validation(map[string]string{"expression": err.Error()})
	}
	if err := requireReason(req.Reason); err != nil {
		return err
	}

	ctx := r.Context()
	var saved formulaItem
	status := http.StatusOK
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		var subjectExists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM subjects WHERE id = ?", subjectID).Scan(&subjectExists); err != nil {
			return err
		}
		if subjectExists == 0 {
			return httpx.NotFound("科目が見つかりません")
		}

		drivers, err := listDrivers(ctx, tx, activityID)
		if err != nil {
			return err
		}
		known := slices.Clone(reservedIdents)
		for _, d := range drivers {
			known = append(known, d.Code)
		}
		var unknown []string
		for _, id := range parsed.Idents() {
			if !slices.Contains(known, id) {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			return httpx.Validation(map[string]string{"expression": "未定義のドライバーが使われています: " + strings.Join(unknown, ", ")})
		}

		before, found, err := findFormula(ctx, tx, activityID, subjectID)
		if err != nil {
			return err
		}
		if found {
			if _, err := tx.ExecContext(ctx, "UPDATE activity_formulas SET expression = ? WHERE id = ?", expr, before.ID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO activity_formulas (activity_id, subject_id, expression) VALUES (?, ?, ?)",
				activityID, subjectID, expr,
			); err != nil {
				return err
			}
			status = http.StatusCreated
		}
		if saved, _, err = findFormula(ctx, tx, activityID, subjectID); err != nil {
			return err
		}
		if found {
			return rec.Update(ctx, "activity_formulas", saved.ID, before, saved)
		}
		return rec.Insert(ctx, "activity_formulas", saved.ID, saved)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, status, saved)
	return nil
}

// deleteFormula は DELETE /api/activities/{id}/formulas/{subject_id}。変更理由が必須。
func (h *Handler) deleteFormula(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	subjectID, err := httpx.PathID(r, "subject_id")
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
		before, found, err := findFormula(ctx, tx, activityID, subjectID)
		if err != nil {
			return err
		}
		if !found {
			return httpx.NotFound("計算式が見つかりません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity_formulas WHERE id = ?", before.ID); err != nil {
			return err
		}
		return rec.Delete(ctx, "activity_formulas", before.ID, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
