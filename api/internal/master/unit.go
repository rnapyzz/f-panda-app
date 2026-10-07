package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/codes"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// unitCodePrefix は自動採番するユニットコードの接頭辞（UNIT-0001 の形式）。
const unitCodePrefix = "UNIT-"

// unitTypes はユニットの種別。service = サービス（プロフィットセンター）、cost_center = 共通費、corporate = 管理部門。
var unitTypes = []string{"service", "cost_center", "corporate"}

// unit はユニット（施策を束ねる単位）。セグメントと組織の末端ノードに1つずつ所属する。
type unit struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	UnitType       string `json:"unit_type"`
	SegmentID      int64  `json:"segment_id"`
	OrganizationID int64  `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
	// IsArchived は廃止（統合したユニットなど）。一覧・選択肢に出さず、施策を所属させられない（docs/plan.md「2.15」）
	IsArchived bool `json:"is_archived"`
	timestamps
}

type unitRequest struct {
	Code           string `json:"code"` // 作成時は空なら自動採番、更新時は空なら変更しない
	Name           string `json:"name"`
	UnitType       string `json:"unit_type"` // 省略時は service
	SegmentID      int64  `json:"segment_id"`
	OrganizationID int64  `json:"organization_id"`
	OwnerUserID    *int64 `json:"owner_user_id"`
	reasonRequest
}

const unitSelect = "SELECT id, code, name, unit_type, segment_id, organization_id, owner_user_id, is_archived, created_at, updated_at FROM units"

func scanUnit(row interface{ Scan(...any) error }) (unit, error) {
	var f unit
	var owner sql.NullInt64
	err := row.Scan(&f.ID, &f.Code, &f.Name, &f.UnitType, &f.SegmentID, &f.OrganizationID, &owner, &f.IsArchived, &f.CreatedAt, &f.UpdatedAt)
	f.OwnerUserID = dbx.PtrInt64(owner)
	return f, err
}

func findUnit(ctx context.Context, q dbx.Querier, id int64, lock string) (unit, error) {
	f, err := scanUnit(q.QueryRowContext(ctx, unitSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return unit{}, notFound("ユニット")
	}
	return f, err
}

// listUnits は GET /api/units。廃止したユニットは include_archived=true のときだけ含める。
func (h *Handler) listUnits(w http.ResponseWriter, r *http.Request) error {
	where := " WHERE NOT is_archived"
	if r.URL.Query().Get("include_archived") == "true" {
		where = ""
	}
	rows, err := h.db.QueryContext(r.Context(), unitSelect+where+" ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []unit
	for rows.Next() {
		f, err := scanUnit(rows)
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

// getUnit は GET /api/units/{id}。
func (h *Handler) getUnit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	f, err := findUnit(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, f)
	return nil
}

// createUnit は POST /api/units。
func (h *Handler) createUnit(w http.ResponseWriter, r *http.Request) error {
	var req unitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, err := validateUnitRequest(&req, true)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created unit
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := h.checkUnitRefs(ctx, tx, req); err != nil {
			return err
		}
		if req.Code == "" {
			next, err := codes.Next(ctx, tx, "units", unitCodePrefix, nil)
			if err != nil {
				return err
			}
			req.Code = next
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO units (code, name, unit_type, segment_id, organization_id, owner_user_id) VALUES (?, ?, ?, ?, ?, ?)",
			req.Code, name, req.UnitType, req.SegmentID, req.OrganizationID, dbx.NullInt64(req.OwnerUserID),
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "このコードは既に使われています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findUnit(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "units", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateUnit は PUT /api/units/{id}。
func (h *Handler) updateUnit(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req unitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, err := validateUnitRequest(&req, true) // 空なら今のコードのまま
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated unit
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findUnit(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if err := h.checkUnitRefs(ctx, tx, req); err != nil {
			return err
		}
		if req.Code == "" {
			req.Code = before.Code
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE units SET code = ?, name = ?, unit_type = ?, segment_id = ?, organization_id = ?, owner_user_id = ? WHERE id = ?",
			req.Code, name, req.UnitType, req.SegmentID, req.OrganizationID, dbx.NullInt64(req.OwnerUserID), id,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "このコードは既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findUnit(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "units", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteUnit は DELETE /api/units/{id}。施策が所属している場合は削除できない。
func (h *Handler) deleteUnit(w http.ResponseWriter, r *http.Request) error {
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
		before, err := findUnit(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM activities WHERE unit_id = ?", id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("施策が所属しているため削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM units WHERE id = ?", id); err != nil {
			return deleteError(err, "ユニット")
		}
		return rec.Delete(ctx, "units", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// validateUnitRequest はユニットの入力を検証する。allowEmptyCode なら空のコードを許す（自動採番する）。
func validateUnitRequest(req *unitRequest, allowEmptyCode bool) (string, error) {
	v := httpx.Validator{}
	name := v.Text("name", "名称", req.Name, maxNameLen)
	req.Code = validateCode(v, req.Code, allowEmptyCode)
	if req.UnitType == "" {
		req.UnitType = "service"
	}
	if !slices.Contains(unitTypes, req.UnitType) {
		v.Add("unit_type", "種別は service（サービス）/ cost_center（共通費）/ corporate（管理部門）のいずれかを指定してください")
	}
	if req.SegmentID <= 0 {
		v.Add("segment_id", "セグメントを選択してください")
	}
	if req.OrganizationID <= 0 {
		v.Add("organization_id", "組織を選択してください")
	}
	return name, v.Err()
}

// checkUnitRefs は所属先のセグメント・組織が存在する末端ノードであること、担当者が存在することを確認する。
// 所属先の行を FOR UPDATE でロックし、同時に子ノードが追加されるのを防ぐ。
func (h *Handler) checkUnitRefs(ctx context.Context, tx *sql.Tx, req unitRequest) error {
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
