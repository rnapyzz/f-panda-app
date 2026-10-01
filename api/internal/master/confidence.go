package master

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 確度の段階（docs/plan.md「2.8 確度とリスク」）。名前・標準の確率・判定基準を持つ。
// 施策（activities.confidence_level）はコードで参照するため、コードは作成後に変更できない。

var confidenceCodePattern = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// confidenceLevel は確度の段階。
type confidenceLevel struct {
	ID        int64       `json:"id"`
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	Rate      json.Number `json:"rate"` // 標準の確率（0〜1）
	Criteria  string      `json:"criteria"`
	SortOrder int         `json:"sort_order"`
	timestamps
}

type confidenceRequest struct {
	Code      string      `json:"code"` // 作成時のみ
	Name      string      `json:"name"`
	Rate      json.Number `json:"rate"`
	Criteria  string      `json:"criteria"`
	SortOrder int         `json:"sort_order"`
	reasonRequest
}

const confidenceSelect = "SELECT id, code, name, rate, COALESCE(criteria, ''), sort_order, created_at, updated_at FROM confidence_levels"

func scanConfidence(row interface{ Scan(...any) error }) (confidenceLevel, error) {
	var c confidenceLevel
	var rate string
	err := row.Scan(&c.ID, &c.Code, &c.Name, &rate, &c.Criteria, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt)
	c.Rate = trimRate(rate)
	return c, err
}

// trimRate は DECIMAL(5,4) の文字列から末尾の 0 を取り除く（"0.8000" → "0.8"）。
func trimRate(s string) json.Number {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "" {
		s = "0"
	}
	return json.Number(s)
}

func findConfidence(ctx context.Context, q dbx.Querier, id int64, lock string) (confidenceLevel, error) {
	c, err := scanConfidence(q.QueryRowContext(ctx, confidenceSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return confidenceLevel{}, notFound("確度の段階")
	}
	return c, err
}

// listConfidenceLevels は GET /api/confidence-levels。表示順・コードの順に返す。
func (h *Handler) listConfidenceLevels(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), confidenceSelect+" ORDER BY sort_order, code")
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []confidenceLevel
	for rows.Next() {
		c, err := scanConfidence(rows)
		if err != nil {
			return err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

func validateConfidence(req *confidenceRequest, creating bool) (name, rate, criteria string, err error) {
	v := httpx.Validator{}
	if creating {
		req.Code = strings.TrimSpace(req.Code)
		if !confidenceCodePattern.MatchString(req.Code) {
			v.Add("code", "コードは英大文字・数字の10文字以内で入力してください（例: A）")
		}
	}
	name = v.Text("name", "名前", req.Name, 50)
	criteria = v.OptionalText("criteria", "判定基準", req.Criteria, 1000)
	x, ok := new(big.Rat).SetString(string(req.Rate))
	switch {
	case !ok:
		v.Add("rate", "標準の確率は 0〜1 の数値で入力してください（例: 0.5）")
	case x.Sign() < 0 || x.Cmp(big.NewRat(1, 1)) > 0:
		v.Add("rate", "標準の確率は 0〜1 の範囲で入力してください")
	case !new(big.Rat).Mul(x, big.NewRat(10000, 1)).IsInt():
		v.Add("rate", "標準の確率は小数点以下4桁までで入力してください")
	default:
		rate = x.FloatString(4)
	}
	return name, rate, criteria, v.Err()
}

// createConfidenceLevel は POST /api/confidence-levels。
func (h *Handler) createConfidenceLevel(w http.ResponseWriter, r *http.Request) error {
	var req confidenceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, rate, criteria, err := validateConfidence(&req, true)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var created confidenceLevel
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO confidence_levels (code, name, rate, criteria, sort_order) VALUES (?, ?, ?, ?, ?)",
			req.Code, name, rate, dbx.NullString(criteria), req.SortOrder)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "このコードは既に使われています"})
		}
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if created, err = findConfidence(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "confidence_levels", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updateConfidenceLevel は PUT /api/confidence-levels/{id}。コードは変更できない。
// 標準の確率を変えると、加重見込・楽観・悲観の集計結果が変わる（金額そのものは満額のまま）。
func (h *Handler) updateConfidenceLevel(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req confidenceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	name, rate, criteria, err := validateConfidence(&req, false)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var updated confidenceLevel
	err = inTx(ctx, h.db, r, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findConfidence(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE confidence_levels SET name = ?, rate = ?, criteria = ?, sort_order = ? WHERE id = ?",
			name, rate, dbx.NullString(criteria), req.SortOrder, id); err != nil {
			return err
		}
		if updated, err = findConfidence(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "confidence_levels", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deleteConfidenceLevel は DELETE /api/confidence-levels/{id}。施策から参照されている段階は削除できない。
func (h *Handler) deleteConfidenceLevel(w http.ResponseWriter, r *http.Request) error {
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
		before, err := findConfidence(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM confidence_levels WHERE id = ?", id); err != nil {
			return deleteError(err, "確度の段階")
		}
		return rec.Delete(ctx, "confidence_levels", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
