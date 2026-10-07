package activity

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"slices"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/formula"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

var driverKinds = []string{"value", "cost", "kpi"}

// driverCodePattern はドライバーの code（計算式で参照する識別子）の形式。
var driverCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

// driver は施策のドライバー定義。
type driver struct {
	ID         int64  `json:"id"`
	ActivityID int64  `json:"activity_id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	DriverKind string `json:"driver_kind"`
	Unit       string `json:"unit"`
	SortOrder  int    `json:"sort_order"`
	timestamps
}

type driverRequest struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	DriverKind string `json:"driver_kind"`
	Unit       string `json:"unit"`
	Reason     string `json:"reason"`
}

const driverSelect = "SELECT id, activity_id, code, name, driver_kind, COALESCE(unit, ''), sort_order, created_at, updated_at FROM activity_drivers"

func scanDriver(row interface{ Scan(...any) error }) (driver, error) {
	var d driver
	err := row.Scan(&d.ID, &d.ActivityID, &d.Code, &d.Name, &d.DriverKind, &d.Unit, &d.SortOrder, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func listDrivers(ctx context.Context, q querier, activityID int64) ([]driver, error) {
	rows, err := q.QueryContext(ctx, driverSelect+" WHERE activity_id = ? ORDER BY sort_order, id", activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []driver{}
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func findDriver(ctx context.Context, tx *sql.Tx, activityID, id int64) (driver, error) {
	d, err := scanDriver(tx.QueryRowContext(ctx, driverSelect+" WHERE id = ? AND activity_id = ? FOR UPDATE", id, activityID))
	if errors.Is(err, sql.ErrNoRows) {
		return driver{}, httpx.NotFound("ドライバーが見つかりません")
	}
	return d, err
}

func validateDriver(req driverRequest) (driverRequest, error) {
	v := httpx.Validator{}
	req.Name = v.Text("name", "ドライバー名", req.Name, 100)
	req.Unit = v.OptionalText("unit", "単位", req.Unit, 30)
	switch {
	case !driverCodePattern.MatchString(req.Code):
		v.Add("code", "コードは半角英小文字で始まる、英小文字・数字・アンダースコアの50文字以内で入力してください")
	}
	if !slices.Contains(driverKinds, req.DriverKind) {
		v.Add("driver_kind", "種別は value / cost / kpi のいずれかを指定してください")
	}
	return req, v.Err()
}

// createDriver は POST /api/activities/{id}/drivers。
func (h *Handler) createDriver(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req driverRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req, err = validateDriver(req); err != nil {
		return err
	}

	ctx := r.Context()
	var created driver
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO activity_drivers (activity_id, code, name, driver_kind, unit, sort_order)
			 SELECT ?, ?, ?, ?, ?, COALESCE(MAX(sort_order), 0) + 1 FROM activity_drivers WHERE activity_id = ?`,
			activityID, req.Code, req.Name, req.DriverKind, dbx.NullString(req.Unit), activityID,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "このコードは施策内で既に使われています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findDriver(ctx, tx, activityID, id); err != nil {
			return err
		}
		return rec.Insert(ctx, "activity_drivers", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateDriver は PUT /api/activities/{id}/drivers/{did}。計算式で使われているドライバーの code は変更できない。
func (h *Handler) updateDriver(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "did")
	if err != nil {
		return err
	}
	var req driverRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req, err = validateDriver(req); err != nil {
		return err
	}

	ctx := r.Context()
	var updated driver
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findDriver(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		if before.Code != req.Code {
			used, err := codeUsedInFormulas(ctx, tx, activityID, before.Code)
			if err != nil {
				return err
			}
			if used {
				return httpx.Validation(map[string]string{"code": "計算式で使われているためコードを変更できません"})
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE activity_drivers SET code = ?, name = ?, driver_kind = ?, unit = ? WHERE id = ?",
			req.Code, req.Name, req.DriverKind, dbx.NullString(req.Unit), id,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "このコードは施策内で既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findDriver(ctx, tx, activityID, id); err != nil {
			return err
		}
		return rec.Update(ctx, "activity_drivers", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteDriver は DELETE /api/activities/{id}/drivers/{did}。
// 計算式で使われている、または値が登録されているドライバーは削除できない。
func (h *Handler) deleteDriver(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "did")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		before, err := findDriver(ctx, tx, activityID, id)
		if err != nil {
			return err
		}
		used, err := codeUsedInFormulas(ctx, tx, activityID, before.Code)
		if err != nil {
			return err
		}
		if used {
			return httpx.Conflict("計算式で使われているため削除できません")
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM driver_values WHERE activity_driver_id = ?", id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("ドライバー値が登録されているため削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity_drivers WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "activity_drivers", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// codeUsedInFormulas は施策の内訳の計算式のいずれか（反映しないものも含む）が code を参照しているかを返す。
func codeUsedInFormulas(ctx context.Context, tx *sql.Tx, activityID int64, code string) (bool, error) {
	lines, err := listLines(ctx, tx, activityID, "")
	if err != nil {
		return false, err
	}
	for _, l := range lines {
		if l.Expression == "" {
			continue
		}
		e, err := formula.Parse(l.Expression)
		if err != nil {
			continue
		}
		if slices.Contains(e.Idents(), code) {
			return true, nil
		}
	}
	return false, nil
}

// reorderDrivers は PUT /api/activities/{id}/drivers/order。ドライバーの表示順を、指定した ID の順に並べ替える。
// すべてのドライバーの ID を1回ずつ指定する。表示だけの変更なので変更理由は任意。
func (h *Handler) reorderDrivers(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	activityID, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		IDs    []int64 `json:"ids"`
		Reason string  `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	var items []driver
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if _, err := lockEditable(ctx, tx, u, activityID); err != nil {
			return err
		}
		current, err := listDrivers(ctx, tx, activityID)
		if err != nil {
			return err
		}
		byID := map[int64]driver{}
		for _, d := range current {
			byID[d.ID] = d
		}
		seen := map[int64]bool{}
		for _, id := range req.IDs {
			if _, ok := byID[id]; !ok || seen[id] {
				return httpx.Validation(map[string]string{"ids": "この施策のドライバーを、重複なく指定してください"})
			}
			seen[id] = true
		}
		if len(req.IDs) != len(current) {
			return httpx.Validation(map[string]string{"ids": "すべてのドライバーを指定してください"})
		}
		for i, id := range req.IDs {
			before := byID[id]
			if before.SortOrder == i+1 {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE activity_drivers SET sort_order = ? WHERE id = ?", i+1, id); err != nil {
				return err
			}
			after := before
			after.SortOrder = i + 1
			if err := rec.Update(ctx, "activity_drivers", id, before, after); err != nil {
				return err
			}
		}
		items, err = listDrivers(ctx, tx, activityID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}
