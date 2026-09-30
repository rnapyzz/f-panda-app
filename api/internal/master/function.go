package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// function は機能（施策を束ねる単位）。セグメントと組織の末端ノードに1つずつ所属する。
type function struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	SegmentID      int64  `json:"segment_id"`
	OrganizationID int64  `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
	timestamps
}

type functionRequest struct {
	Name           string `json:"name"`
	SegmentID      int64  `json:"segment_id"`
	OrganizationID int64  `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
	reasonRequest
}

const functionSelect = "SELECT id, name, segment_id, organization_id, owner_user_id, created_at, updated_at FROM functions"

func scanFunction(row interface{ Scan(...any) error }) (function, error) {
	var f function
	var owner sql.NullInt64
	err := row.Scan(&f.ID, &f.Name, &f.SegmentID, &f.OrganizationID, &owner, &f.CreatedAt, &f.UpdatedAt)
	f.OwnerUserID = dbx.PtrInt64(owner)
	return f, err
}

func findFunction(ctx context.Context, q dbx.Querier, id int64, lock string) (function, error) {
	f, err := scanFunction(q.QueryRowContext(ctx, functionSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return function{}, notFound("機能")
	}
	return f, err
}

// listFunctions は GET /api/functions。
func (h *Handler) listFunctions(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), functionSelect+" ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []function
	for rows.Next() {
		f, err := scanFunction(rows)
		if err != nil {
			return err
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// getFunction は GET /api/functions/{id}。
func (h *Handler) getFunction(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	f, err := findFunction(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, f)
	return nil
}

// createFunction は POST /api/functions。
func (h *Handler) createFunction(w http.ResponseWriter, r *http.Request) error {
	var req functionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, err := validateFunctionRequest(req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created function
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := h.checkFunctionRefs(ctx, tx, req); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO functions (name, segment_id, organization_id, owner_user_id) VALUES (?, ?, ?, ?)",
			name, req.SegmentID, req.OrganizationID, dbx.NullInt64(req.OwnerUserID),
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findFunction(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "functions", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateFunction は PUT /api/functions/{id}。
func (h *Handler) updateFunction(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req functionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, err := validateFunctionRequest(req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated function
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findFunction(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if err := h.checkFunctionRefs(ctx, tx, req); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE functions SET name = ?, segment_id = ?, organization_id = ?, owner_user_id = ? WHERE id = ?",
			name, req.SegmentID, req.OrganizationID, dbx.NullInt64(req.OwnerUserID), id,
		); err != nil {
			return err
		}
		if updated, err = findFunction(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "functions", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteFunction は DELETE /api/functions/{id}。施策が所属している場合は削除できない。
func (h *Handler) deleteFunction(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findFunction(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activities WHERE function_id = ?", id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("施策が所属しているため削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM functions WHERE id = ?", id); err != nil {
			return deleteError(err, "機能")
		}
		return rec.Delete(ctx, "functions", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func validateFunctionRequest(req functionRequest) (string, error) {
	v := httpx.Validator{}
	name := v.Text("name", "名称", req.Name, maxNameLen)
	if req.SegmentID <= 0 {
		v.Add("segment_id", "セグメントを選択してください")
	}
	if req.OrganizationID <= 0 {
		v.Add("organization_id", "組織を選択してください")
	}
	return name, v.Err()
}

// checkFunctionRefs は所属先のセグメント・組織が存在する末端ノードであること、担当者が存在することを確認する。
// 所属先の行を FOR UPDATE でロックし、同時に子ノードが追加されるのを防ぐ。
func (h *Handler) checkFunctionRefs(ctx context.Context, tx *sql.Tx, req functionRequest) error {
	v := httpx.Validator{}
	for _, ref := range []struct {
		t     *treeHandler
		field string
		id    int64
	}{
		{h.segments, "segment_id", req.SegmentID},
		{h.organizations, "organization_id", req.OrganizationID},
	} {
		if _, err := ref.t.find(ctx, tx, ref.id, " FOR UPDATE"); err != nil {
			if httpx.IsNotFound(err) {
				v.Add(ref.field, ref.t.label+"が見つかりません")
				continue
			}
			return err
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM "+ref.t.table+" WHERE parent_id = ?", ref.id)
		if err != nil {
			return err
		}
		if n > 0 {
			v.Add(ref.field, "末端の"+ref.t.label+"を選択してください")
		}
	}
	if req.OwnerUserID != nil {
		ok, err := dbx.Exists(ctx, tx, "users", *req.OwnerUserID)
		if err != nil {
			return err
		}
		if !ok {
			v.Add("owner_user_id", "担当者が見つかりません")
		}
	}
	return v.Err()
}
