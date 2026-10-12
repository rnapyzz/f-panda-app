package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// subject は勘定科目。親科目を持てる（同じ区分の科目のみ）。
type subject struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	SortOrder int    `json:"sort_order"`
	// IsRestricted は閲覧制限（docs/plan.md「2.17」）。金額は FP&A と経営陣だけが見られる
	IsRestricted bool `json:"is_restricted"`
	// IsProjectCost は原価の科目、Role は科目の役割（wip_transfer は仕掛品振替の科目。docs/plan.md「2.28」）
	IsProjectCost bool   `json:"is_project_cost"`
	Role          string `json:"role"`
	timestamps
}

type subjectRequest struct {
	ParentID     *int64 `json:"parent_id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	SortOrder    int    `json:"sort_order"`
	IsRestricted bool   `json:"is_restricted"`
	// IsProjectCost・Role は省略すると、作成では false・normal、更新では今のまま
	IsProjectCost *bool   `json:"is_project_cost"`
	Role          *string `json:"role"`
	reasonRequest
}

const subjectSelect = "SELECT id, parent_id, code, name, category, sort_order, is_restricted, is_project_cost, role, created_at, updated_at FROM subjects"

func scanSubject(row interface{ Scan(...any) error }) (subject, error) {
	var s subject
	var parent sql.NullInt64
	err := row.Scan(&s.ID, &parent, &s.Code, &s.Name, &s.Category, &s.SortOrder, &s.IsRestricted, &s.IsProjectCost, &s.Role, &s.CreatedAt, &s.UpdatedAt)
	s.ParentID = dbx.PtrInt64(parent)
	return s, err
}

func findSubject(ctx context.Context, q dbx.Querier, id int64, lock string) (subject, error) {
	s, err := scanSubject(q.QueryRowContext(ctx, subjectSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return subject{}, notFound("科目")
	}
	return s, err
}

// listSubjects は GET /api/subjects。区分（収益→費用）・表示順・コードの順に返す。
func (h *Handler) listSubjects(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), subjectSelect+" ORDER BY category, sort_order, code")
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []subject
	for rows.Next() {
		s, err := scanSubject(rows)
		if err != nil {
			return err
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// getSubject は GET /api/subjects/{id}。
func (h *Handler) getSubject(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	s, err := findSubject(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// createSubject は POST /api/subjects。
func (h *Handler) createSubject(w http.ResponseWriter, r *http.Request) error {
	var req subjectRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	code, name, err := validateSubjectRequest(&req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created subject
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := checkSubjectParent(ctx, tx, 0, req); err != nil {
			return err
		}
		cost, role := wipFlags(req, subject{Role: "normal"})
		if err := validateWipFlags(req.Category, cost, role); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO subjects (parent_id, code, name, category, sort_order, is_restricted, is_project_cost, role) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			dbx.NullInt64(req.ParentID), code, name, req.Category, req.SortOrder, req.IsRestricted, cost, role,
		)
		if err := subjectDuplicate(err); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findSubject(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "subjects", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateSubject は PUT /api/subjects/{id}。
func (h *Handler) updateSubject(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req subjectRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	code, name, err := validateSubjectRequest(&req)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated subject
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findSubject(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if err := checkSubjectParent(ctx, tx, id, req); err != nil {
			return err
		}
		if req.Category != before.Category {
			n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM subjects WHERE parent_id = ?", id)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.Validation(map[string]string{"category": "子科目がある科目の区分は変更できません"})
			}
		}
		cost, role := wipFlags(req, before)
		if err := validateWipFlags(req.Category, cost, role); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE subjects SET parent_id = ?, code = ?, name = ?, category = ?, sort_order = ?, is_restricted = ?, is_project_cost = ?, role = ? WHERE id = ?",
			dbx.NullInt64(req.ParentID), code, name, req.Category, req.SortOrder, req.IsRestricted, cost, role, id,
		)
		if err := subjectDuplicate(err); err != nil {
			return err
		}
		if updated, err = findSubject(ctx, tx, id, ""); err != nil {
			return err
		}
		if err := rec.Update(ctx, "subjects", id, before, updated); err != nil {
			return err
		}
		if before.IsProjectCost != updated.IsProjectCost || before.Role != updated.Role {
			// 原価の科目・仕掛品振替の科目を変えたら、検収月に一括の施策の振替を計算し直す（docs/plan.md「2.28」）
			return calc.RecalculateWipAll(ctx, tx, rec)
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteSubject は DELETE /api/subjects/{id}。子科目や金額データから参照されている場合は削除できない。
func (h *Handler) deleteSubject(w http.ResponseWriter, r *http.Request) error {
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
		before, err := findSubject(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM subjects WHERE parent_id = ?", id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("子科目があるため削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM subjects WHERE id = ?", id); err != nil {
			return deleteError(err, "科目")
		}
		return rec.Delete(ctx, "subjects", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// wipFlags は、原価の科目・役割の入力（省略なら before のまま）を返す。
func wipFlags(req subjectRequest, before subject) (cost bool, role string) {
	cost, role = before.IsProjectCost, before.Role
	if req.IsProjectCost != nil {
		cost = *req.IsProjectCost
	}
	if req.Role != nil {
		role = *req.Role
	}
	return cost, role
}

// validateWipFlags は原価の科目・仕掛品振替の科目の設定を検証する。どちらも費用の科目だけで、両方にはできない。
func validateWipFlags(category string, cost bool, role string) error {
	v := httpx.Validator{}
	switch {
	case role != "normal" && role != "wip_transfer":
		v.Add("role", "科目の役割は normal か wip_transfer（仕掛品振替）を指定してください")
	case role == "wip_transfer" && category != "expense":
		v.Add("role", "仕掛品振替の科目は費用の科目にしてください")
	case role == "wip_transfer" && cost:
		v.Add("is_project_cost", "仕掛品振替の科目は原価の科目にできません")
	}
	if cost && category != "expense" {
		v.Add("is_project_cost", "原価の科目は費用の科目にしてください")
	}
	return v.Err()
}

// subjectDuplicate は一意制約の違反を、どの項目かが分かる入力エラーにする。
func subjectDuplicate(err error) error {
	if dbx.ErrNo(err) != dbx.ErrDuplicateEntry {
		return err
	}
	if strings.Contains(err.Error(), "uq_subjects_wip_transfer") {
		return httpx.Validation(map[string]string{"role": "仕掛品振替の科目はすでにほかの科目に指定されています（1つだけ指定できます）"})
	}
	return httpx.Validation(map[string]string{"code": "この科目コードは既に使われています"})
}

func validateSubjectRequest(req *subjectRequest) (code, name string, err error) {
	v := httpx.Validator{}
	code = v.Text("code", "科目コード", req.Code, 50)
	name = v.Text("name", "科目名", req.Name, maxNameLen)
	if req.Category != "revenue" && req.Category != "expense" {
		v.Add("category", "区分は revenue（収益）か expense（費用）を指定してください")
	}
	return code, name, v.Err()
}

// checkSubjectParent は親科目が存在し、同じ区分で、循環しないことを確認する。id は更新対象（新規作成時は 0）。
func checkSubjectParent(ctx context.Context, tx *sql.Tx, id int64, req subjectRequest) error {
	if req.ParentID == nil {
		return nil
	}
	if *req.ParentID == id {
		return httpx.Validation(map[string]string{"parent_id": "自分自身を親にはできません"})
	}
	parent, err := findSubject(ctx, tx, *req.ParentID, " FOR SHARE")
	if err != nil {
		if httpx.IsNotFound(err) {
			return httpx.Validation(map[string]string{"parent_id": "親科目が見つかりません"})
		}
		return err
	}
	if parent.Category != req.Category {
		return httpx.Validation(map[string]string{"parent_id": "親科目と区分を揃えてください"})
	}
	if id == 0 {
		return nil
	}
	// 親をたどって自分自身に行き着くなら循環している。
	n, err := dbx.Count(ctx, tx, `
		WITH RECURSIVE anc AS (
			SELECT id, parent_id FROM subjects WHERE id = ?
			UNION ALL
			SELECT s.id, s.parent_id FROM subjects s JOIN anc ON s.id = anc.parent_id
		)
		SELECT COUNT(*) FROM anc WHERE id = ?`, *req.ParentID, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpx.Validation(map[string]string{"parent_id": "配下の科目を親にはできません"})
	}
	return nil
}
