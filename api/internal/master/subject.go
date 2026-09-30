package master

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
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
	timestamps
}

type subjectRequest struct {
	ParentID  *int64 `json:"parent_id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	SortOrder int    `json:"sort_order"`
	reasonRequest
}

const subjectSelect = "SELECT id, parent_id, code, name, category, sort_order, created_at, updated_at FROM subjects"

func scanSubject(row interface{ Scan(...any) error }) (subject, error) {
	var s subject
	var parent sql.NullInt64
	err := row.Scan(&s.ID, &parent, &s.Code, &s.Name, &s.Category, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt)
	s.ParentID = ptrInt64(parent)
	return s, err
}

func findSubject(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64, lock string) (subject, error) {
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
	writeList(w, items)
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
	writeItem(w, http.StatusOK, s)
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
		res, err := tx.ExecContext(ctx,
			"INSERT INTO subjects (parent_id, code, name, category, sort_order) VALUES (?, ?, ?, ?, ?)",
			nullInt64(req.ParentID), code, name, req.Category, req.SortOrder,
		)
		if mysqlErrNo(err) == errDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "この科目コードは既に使われています"})
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
	writeItem(w, http.StatusCreated, created)
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
			n, err := count(ctx, tx, "SELECT COUNT(*) FROM subjects WHERE parent_id = ?", id)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.Validation(map[string]string{"category": "子科目がある科目の区分は変更できません"})
			}
		}
		_, err = tx.ExecContext(ctx,
			"UPDATE subjects SET parent_id = ?, code = ?, name = ?, category = ?, sort_order = ? WHERE id = ?",
			nullInt64(req.ParentID), code, name, req.Category, req.SortOrder, id,
		)
		if mysqlErrNo(err) == errDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "この科目コードは既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findSubject(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "subjects", id, before, updated)
	})
	if err != nil {
		return err
	}
	writeItem(w, http.StatusOK, updated)
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
		n, err := count(ctx, tx, "SELECT COUNT(*) FROM subjects WHERE parent_id = ?", id)
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

func validateSubjectRequest(req *subjectRequest) (code, name string, err error) {
	v := validator{}
	code = v.text("code", "科目コード", req.Code, 50)
	name = v.text("name", "科目名", req.Name, maxNameLen)
	if req.Category != "revenue" && req.Category != "expense" {
		v.add("category", "区分は revenue（収益）か expense（費用）を指定してください")
	}
	return code, name, v.err()
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
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
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
	n, err := count(ctx, tx, `
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
