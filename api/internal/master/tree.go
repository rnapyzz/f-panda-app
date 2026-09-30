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

// treeHandler は階層構造を持つマスタ（組織・セグメント）の API。
//
// ユニット（units）は末端ノードにのみ所属できるため、次を守る:
//   - ユニットが所属しているノードの下には子ノードを作れない（移動先にもできない）
//   - 子ノードを持つノードにはユニットを所属させられない（units 側で検証）
type treeHandler struct {
	db         *sql.DB
	table      string // organizations / segments
	label      string // 組織 / セグメント
	unitColumn string // units テーブルでこのマスタを参照する列
}

type treeNode struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	SortOrder int    `json:"sort_order"`
	timestamps
}

type treeRequest struct {
	ParentID  *int64 `json:"parent_id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	reasonRequest
}

const maxNameLen = 100

func (t *treeHandler) selectSQL() string {
	return "SELECT id, parent_id, name, level, sort_order, created_at, updated_at FROM " + t.table
}

func scanTreeNode(row interface{ Scan(...any) error }) (treeNode, error) {
	var n treeNode
	var parent sql.NullInt64
	err := row.Scan(&n.ID, &parent, &n.Name, &n.Level, &n.SortOrder, &n.CreatedAt, &n.UpdatedAt)
	n.ParentID = dbx.PtrInt64(parent)
	return n, err
}

func (t *treeHandler) find(ctx context.Context, q dbx.Querier, id int64, lock string) (treeNode, error) {
	n, err := scanTreeNode(q.QueryRowContext(ctx, t.selectSQL()+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return treeNode{}, notFound(t.label)
	}
	return n, err
}

// list は GET /api/{organizations|segments}。階層レベル・表示順の順に全件を返す。
func (t *treeHandler) list(w http.ResponseWriter, r *http.Request) error {
	rows, err := t.db.QueryContext(r.Context(), t.selectSQL()+" ORDER BY level, sort_order, id")
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []treeNode
	for rows.Next() {
		n, err := scanTreeNode(rows)
		if err != nil {
			return err
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// get は GET /api/{organizations|segments}/{id}。
func (t *treeHandler) get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	n, err := t.find(r.Context(), t.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, n)
	return nil
}

// create は POST /api/{organizations|segments}。
func (t *treeHandler) create(w http.ResponseWriter, r *http.Request) error {
	var req treeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "名称", req.Name, maxNameLen)
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var created treeNode
	err := inTx(ctx, t.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		level := 1
		if req.ParentID != nil {
			parent, err := t.lockParent(ctx, tx, *req.ParentID)
			if err != nil {
				return err
			}
			level = parent.Level + 1
		}

		res, err := tx.ExecContext(ctx,
			"INSERT INTO "+t.table+" (parent_id, name, level, sort_order) VALUES (?, ?, ?, ?)",
			dbx.NullInt64(req.ParentID), name, level, req.SortOrder,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = t.find(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, t.table, id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// update は PUT /api/{organizations|segments}/{id}。名称・表示順・親（移動）を更新する。
func (t *treeHandler) update(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req treeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "名称", req.Name, maxNameLen)
	if req.ParentID != nil && *req.ParentID == id {
		v.Add("parent_id", "自分自身を親にはできません")
	}
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var updated treeNode
	err = inTx(ctx, t.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := t.find(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}

		level := before.Level
		if !sameParent(before.ParentID, req.ParentID) {
			level = 1
			if req.ParentID != nil {
				descendants, err := t.descendantIDs(ctx, tx, id)
				if err != nil {
					return err
				}
				if descendants[*req.ParentID] {
					return httpx.Validation(map[string]string{"parent_id": "配下の" + t.label + "を親にはできません"})
				}
				parent, err := t.lockParent(ctx, tx, *req.ParentID)
				if err != nil {
					return err
				}
				level = parent.Level + 1
			}
		}

		if _, err := tx.ExecContext(ctx,
			"UPDATE "+t.table+" SET parent_id = ?, name = ?, sort_order = ?, level = ? WHERE id = ?",
			dbx.NullInt64(req.ParentID), name, req.SortOrder, level, id,
		); err != nil {
			return err
		}
		// 移動した場合は配下のノードの階層レベルも合わせてずらす。
		if delta := level - before.Level; delta != 0 {
			if _, err := tx.ExecContext(ctx, `
				WITH RECURSIVE sub AS (
					SELECT id FROM `+t.table+` WHERE parent_id = ?
					UNION ALL
					SELECT c.id FROM `+t.table+` c JOIN sub ON c.parent_id = sub.id
				)
				UPDATE `+t.table+` t JOIN sub ON t.id = sub.id SET t.level = t.level + ?`,
				id, delta,
			); err != nil {
				return err
			}
		}

		if updated, err = t.find(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, t.table, id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// delete は DELETE /api/{organizations|segments}/{id}。子ノードやユニットから参照されている場合は削除できない。
func (t *treeHandler) delete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(ctx, t.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := t.find(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM "+t.table+" WHERE parent_id = ?", id)
		if err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("配下に" + t.label + "があるため削除できません")
		}
		if n, err = dbx.Count(ctx, tx, "SELECT COUNT(*) FROM units WHERE "+t.unitColumn+" = ?", id); err != nil {
			return err
		}
		if n > 0 {
			return httpx.Conflict("ユニットが所属しているため削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t.table+" WHERE id = ?", id); err != nil {
			return deleteError(err, t.label)
		}
		return rec.Delete(ctx, t.table, id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// lockParent は親にするノードを行ロック付きで取得し、ユニットが所属していないことを確認する。
func (t *treeHandler) lockParent(ctx context.Context, tx *sql.Tx, parentID int64) (treeNode, error) {
	parent, err := t.find(ctx, tx, parentID, " FOR UPDATE")
	if err != nil {
		if httpx.IsNotFound(err) {
			return treeNode{}, httpx.Validation(map[string]string{"parent_id": "親の" + t.label + "が見つかりません"})
		}
		return treeNode{}, err
	}
	n, err := dbx.Count(ctx, tx, "SELECT COUNT(*) FROM units WHERE "+t.unitColumn+" = ?", parentID)
	if err != nil {
		return treeNode{}, err
	}
	if n > 0 {
		return treeNode{}, httpx.Validation(map[string]string{"parent_id": "ユニットが所属している" + t.label + "の下には追加できません"})
	}
	return parent, nil
}

// descendantIDs は id の配下（子孫）のノード ID を返す。
func (t *treeHandler) descendantIDs(ctx context.Context, tx *sql.Tx, id int64) (map[int64]bool, error) {
	rows, err := tx.QueryContext(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM `+t.table+` WHERE parent_id = ?
			UNION ALL
			SELECT c.id FROM `+t.table+` c JOIN sub ON c.parent_id = sub.id
		)
		SELECT id FROM sub`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		ids[d] = true
	}
	return ids, rows.Err()
}

func sameParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
