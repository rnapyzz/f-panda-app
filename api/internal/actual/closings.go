package actual

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// 年度の締め（docs/plan.md「2.14」）。締めた年度の月は、実績の取込・再割当・未割当の割当ができない。

type closing struct {
	FiscalYear   int       `json:"fiscal_year"`
	ClosedAt     time.Time `json:"closed_at"`
	ClosedBy     int64     `json:"closed_by"`
	ClosedByName string    `json:"closed_by_name"`
}

// fiscalYearOf は年月（YYYY-MM）の年度（4月開始）を返す。
func fiscalYearOf(month string) int {
	y, _ := strconv.Atoi(month[:4])
	m, _ := strconv.Atoi(month[5:7])
	if m < 4 {
		return y - 1
	}
	return y
}

// closedYears は締めた年度の集合を返す。
func closedYears(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (map[int]bool, error) {
	rows, err := q.QueryContext(ctx, "SELECT fiscal_year FROM fiscal_year_closings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var fy int
		if err := rows.Scan(&fy); err != nil {
			return nil, err
		}
		out[fy] = true
	}
	return out, rows.Err()
}

// closedMonths は months のうち、締めた年度の月を返す。
func closedMonths(months []string, closed map[int]bool) []string {
	var out []string
	for _, m := range months {
		if closed[fiscalYearOf(m)] {
			out = append(out, m)
		}
	}
	return out
}

// notClosedCondition は、target_month が締めた年度の月でないことを表す SQL の条件。
const notClosedCondition = `NOT EXISTS (
	SELECT 1 FROM fiscal_year_closings c
	WHERE target_month BETWEEN MAKEDATE(c.fiscal_year, 1) + INTERVAL 3 MONTH AND MAKEDATE(c.fiscal_year + 1, 1) + INTERVAL 2 MONTH)`

const closingSelect = `
	SELECT c.fiscal_year, c.closed_at, c.closed_by, u.name
	FROM fiscal_year_closings c JOIN users u ON u.id = c.closed_by`

func scanClosing(row interface{ Scan(...any) error }) (closing, error) {
	var c closing
	err := row.Scan(&c.FiscalYear, &c.ClosedAt, &c.ClosedBy, &c.ClosedByName)
	return c, err
}

// listClosings は GET /api/fiscal-years/closings。締めた年度の一覧（新しい年度から）。
func (h *Handler) listClosings(w http.ResponseWriter, r *http.Request) error {
	rows, err := h.db.QueryContext(r.Context(), closingSelect+" ORDER BY c.fiscal_year DESC")
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []closing
	for rows.Next() {
		c, err := scanClosing(rows)
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

func pathFiscalYear(r *http.Request) (int, error) {
	fy, err := strconv.Atoi(r.PathValue("fy"))
	if err != nil || fy < 2000 || fy > 2100 {
		return 0, httpx.BadRequest("年度は 2000〜2100 の数値で指定してください")
	}
	return fy, nil
}

// closeYear は POST /api/fiscal-years/{fy}/close。FP&A のみ。締めに条件はない。
func (h *Handler) closeYear(w http.ResponseWriter, r *http.Request) error {
	fy, err := pathFiscalYear(r)
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
	ctx := r.Context()
	var created closing
	err = inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO fiscal_year_closings (fiscal_year, closed_by) VALUES (?, ?)", fy, u.ID)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Conflict(fmt.Sprintf("%d年度は既に締めています", fy))
		}
		if err != nil {
			return err
		}
		if created, err = scanClosing(tx.QueryRowContext(ctx, closingSelect+" WHERE c.fiscal_year = ?", fy)); err != nil {
			return err
		}
		return rec.Insert(ctx, "fiscal_year_closings", int64(fy), created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, created)
	return nil
}

// reopenYear は POST /api/fiscal-years/{fy}/reopen。FP&A のみ。締めの解除は理由が必須。
func (h *Handler) reopenYear(w http.ResponseWriter, r *http.Request) error {
	fy, err := pathFiscalYear(r)
	if err != nil {
		return err
	}
	var req reasonRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Reason) == "" {
		return httpx.Validation(map[string]string{"reason": "締めの解除には変更理由の入力が必要です"})
	}
	ctx := r.Context()
	err = inTx(r, h.db, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := scanClosing(tx.QueryRowContext(ctx, closingSelect+" WHERE c.fiscal_year = ? FOR UPDATE", fy))
		if errors.Is(err, sql.ErrNoRows) {
			return httpx.Conflict(fmt.Sprintf("%d年度は締めていません", fy))
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM fiscal_year_closings WHERE fiscal_year = ?", fy); err != nil {
			return err
		}
		return rec.Delete(ctx, "fiscal_year_closings", int64(fy), before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
