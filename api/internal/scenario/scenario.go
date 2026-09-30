// Package scenario はシナリオと、シナリオごとの数値（ドライバー値・金額・想定条件）の API を提供する。
//
// シナリオの作成・名称変更・ロック/ロック解除は FP&A（fpa_admin）のみ。参照はログインユーザー全員。
// 数値の入力は施策の編集権限を持つユーザーが、ロックされていない実績以外のシナリオに対して行える。
//
// シナリオは削除できない（変更履歴がシナリオを参照するため）。
package scenario

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

var scenarioKinds = []string{"budget", "forecast", "actual", "optimistic", "pessimistic", "other"}

// Handler はシナリオ API のハンドラー。
type Handler struct {
	db *sql.DB
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Register はルートを登録する。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler, requireAdmin func(http.Handler) http.Handler) {
	read := func(f httpx.HandlerFunc) http.Handler { return requireAuth(httpx.Handle(f)) }
	write := func(f httpx.HandlerFunc) http.Handler { return requireAuth(requireAdmin(httpx.Handle(f))) }

	mux.Handle("GET /api/scenarios", read(h.list))
	mux.Handle("POST /api/scenarios", write(h.create))
	mux.Handle("GET /api/scenarios/{id}", read(h.get))
	mux.Handle("PUT /api/scenarios/{id}", write(h.update))
	mux.Handle("POST /api/scenarios/{id}/lock", write(h.lock))
	mux.Handle("POST /api/scenarios/{id}/unlock", write(h.unlock))

	// 数値の入力は施策ごとに権限を判定する
	mux.Handle("GET /api/scenarios/{id}/activities/{aid}", read(h.getValues))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/driver-values", read(h.putDriverValues))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/amounts", read(h.putAmounts))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/condition", read(h.putCondition))
}

// Scenario はシナリオ。
type Scenario struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	ScenarioKind   string    `json:"scenario_kind"`
	FiscalYear     int       `json:"fiscal_year"`
	BaseScenarioID *int64    `json:"base_scenario_id"`
	IsLocked       bool      `json:"is_locked"`
	CreatedBy      int64     `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const scenarioSelect = "SELECT id, name, scenario_kind, fiscal_year, base_scenario_id, is_locked, created_by, created_at, updated_at FROM scenarios"

func scanScenario(row interface{ Scan(...any) error }) (Scenario, error) {
	var s Scenario
	var base sql.NullInt64
	err := row.Scan(&s.ID, &s.Name, &s.ScenarioKind, &s.FiscalYear, &base, &s.IsLocked, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.BaseScenarioID = dbx.PtrInt64(base)
	return s, err
}

func findScenario(ctx context.Context, q dbx.Querier, id int64, lock string) (Scenario, error) {
	s, err := scanScenario(q.QueryRowContext(ctx, scenarioSelect+" WHERE id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Scenario{}, httpx.NotFound("シナリオが見つかりません")
	}
	return s, err
}

type createRequest struct {
	Name           string `json:"name"`
	ScenarioKind   string `json:"scenario_kind"`
	FiscalYear     int    `json:"fiscal_year"`
	BaseScenarioID *int64 `json:"base_scenario_id"`
	Reason         string `json:"reason"`
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

func currentUser(r *http.Request) (auth.User, error) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return auth.User{}, httpx.Unauthorized("ログインしてください")
	}
	return u, nil
}

// list は GET /api/scenarios。fiscal_year / scenario_kind で絞り込める。新しい年度・新しい順に返す。
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	var where []string
	var args []any
	if s := q.Get("fiscal_year"); s != "" {
		y, err := strconv.Atoi(s)
		if err != nil {
			return httpx.BadRequest("fiscal_year は数値で指定してください")
		}
		where = append(where, "fiscal_year = ?")
		args = append(args, y)
	}
	if s := q.Get("scenario_kind"); s != "" {
		where = append(where, "scenario_kind = ?")
		args = append(args, s)
	}
	query := scenarioSelect
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY fiscal_year DESC, id DESC"

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []Scenario
	for rows.Next() {
		s, err := scanScenario(rows)
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

// get は GET /api/scenarios/{id}。
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	s, err := findScenario(r.Context(), h.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// create は POST /api/scenarios。base_scenario_id を指定すると、そのシナリオの数値
// （ドライバー値・金額・想定条件）をすべて複製する。複製元は同じ年度のシナリオに限る。
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "シナリオ名", req.Name, 100)
	if !slices.Contains(scenarioKinds, req.ScenarioKind) {
		v.Add("scenario_kind", "種別は "+strings.Join(scenarioKinds, " / ")+" のいずれかを指定してください")
	}
	if req.FiscalYear < 2000 || req.FiscalYear > 2100 {
		v.Add("fiscal_year", "年度を正しく入力してください（例: 2026）")
	}
	if req.ScenarioKind == "actual" && req.BaseScenarioID != nil {
		v.Add("base_scenario_id", "実績シナリオは複製して作成できません（実績は取込で登録します）")
	}
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var created Scenario
	err = audit.InTx(ctx, h.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		if req.BaseScenarioID != nil {
			base, err := findScenario(ctx, tx, *req.BaseScenarioID, " FOR SHARE")
			if httpx.IsNotFound(err) {
				return httpx.Validation(map[string]string{"base_scenario_id": "複製元のシナリオが見つかりません"})
			}
			if err != nil {
				return err
			}
			if base.FiscalYear != req.FiscalYear {
				return httpx.Validation(map[string]string{"base_scenario_id": "複製元は同じ年度のシナリオを選んでください"})
			}
		}

		res, err := tx.ExecContext(ctx,
			"INSERT INTO scenarios (name, scenario_kind, fiscal_year, base_scenario_id, created_by) VALUES (?, ?, ?, ?, ?)",
			name, req.ScenarioKind, req.FiscalYear, dbx.NullInt64(req.BaseScenarioID), u.ID,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"name": "このシナリオ名は既に使われています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}

		copied := map[string]int64{}
		if req.BaseScenarioID != nil {
			for _, c := range []struct{ table, columns string }{
				{"driver_values", "activity_driver_id, target_month, value, is_provisional, provisional_reason"},
				{"budget_facts", "activity_id, subject_id, target_month, amount, source, is_provisional, provisional_reason"},
				{"scenario_conditions", "activity_id, description"},
			} {
				res, err := tx.ExecContext(ctx,
					"INSERT INTO "+c.table+" (scenario_id, "+c.columns+") SELECT ?, "+c.columns+" FROM "+c.table+" WHERE scenario_id = ?",
					id, *req.BaseScenarioID,
				)
				if err != nil {
					return err
				}
				copied[c.table], _ = res.RowsAffected()
			}
		}

		if created, err = findScenario(ctx, tx, id, ""); err != nil {
			return err
		}
		// 複製した行は件数だけを記録する（行ごとの監査ログは複製元に残っている）。
		return rec.Insert(ctx, "scenarios", id, struct {
			Scenario
			Copied map[string]int64 `json:"copied,omitempty"`
		}{created, copied})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// update は PUT /api/scenarios/{id}。名称のみ変更できる。
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	v := httpx.Validator{}
	name := v.Text("name", "シナリオ名", req.Name, 100)
	if err := v.Err(); err != nil {
		return err
	}

	ctx := r.Context()
	var updated Scenario
	err = audit.InTx(ctx, h.db, u.ID, &id, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findScenario(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE scenarios SET name = ? WHERE id = ?", name, id)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"name": "このシナリオ名は既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findScenario(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "scenarios", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// lock は POST /api/scenarios/{id}/lock。ロックしたシナリオの数値は変更できなくなる。
func (h *Handler) lock(w http.ResponseWriter, r *http.Request) error {
	return h.setLocked(w, r, true)
}

// unlock は POST /api/scenarios/{id}/unlock。確定した版を変更可能に戻すため、変更理由が必須。
func (h *Handler) unlock(w http.ResponseWriter, r *http.Request) error {
	return h.setLocked(w, r, false)
}

func (h *Handler) setLocked(w http.ResponseWriter, r *http.Request, locked bool) error {
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
	if !locked && strings.TrimSpace(req.Reason) == "" {
		return httpx.Validation(map[string]string{"reason": "ロック解除には変更理由の入力が必要です"})
	}

	ctx := r.Context()
	var updated Scenario
	err = audit.InTx(ctx, h.db, u.ID, &id, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findScenario(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if before.IsLocked == locked {
			if locked {
				return httpx.Conflict("このシナリオは既にロックされています")
			}
			return httpx.Conflict("このシナリオはロックされていません")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET is_locked = ? WHERE id = ?", locked, id); err != nil {
			return err
		}
		if updated, err = findScenario(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "scenarios", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}
