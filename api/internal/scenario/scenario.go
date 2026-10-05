// Package scenario はシナリオと、シナリオごとの数値（ドライバー値・金額・想定条件）の API を提供する。
//
// シナリオの金額は、決算確定月（actual_through）以前の月は実績、それより後の月は計画値とする（docs/plan.md「2.6 シナリオ」）。
// 実績はシナリオに属さない実績データ（actual_facts）で、ロック時に決算確定月以前の実績をシナリオに保存して固定する（scenario_actuals）。
//
// シナリオの作成・変更・作成中の指定・ロック/ロック解除、実績の取込は FP&A（fpa_admin）のみ。参照はログインユーザー全員。
// 数値の入力は、作成中のシナリオなら施策の編集権限を持つユーザー、それ以外のロックされていないシナリオは FP&A のみが行える。
//
// シナリオは削除できない（変更履歴がシナリオを参照するため）。
package scenario

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/calc"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// planRoles はシナリオのエイリアス（期初計画・修正計画・最新見込）。年度ごとに1つのシナリオにだけ付けられる。
var planRoles = map[string]string{"initial": "期初計画", "revised": "修正計画", "latest": "最新見込"}

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
	mux.Handle("GET /api/scenarios/active", read(h.active))
	mux.Handle("GET /api/scenarios/{id}/activity-status", read(h.activityStatuses))
	mux.Handle("POST /api/scenarios", write(h.create))
	mux.Handle("GET /api/scenarios/{id}", read(h.get))
	mux.Handle("PUT /api/scenarios/{id}", write(h.update))
	mux.Handle("POST /api/scenarios/{id}/activate", write(h.activate))
	mux.Handle("POST /api/scenarios/{id}/lock", write(h.lock))
	mux.Handle("POST /api/scenarios/{id}/unlock", write(h.unlock))

	mux.Handle("POST /api/actuals/import", write(h.importActuals))
	mux.Handle("GET /api/actuals/months", read(h.actualMonths))

	// 数値の入力は施策ごとに権限を判定する
	mux.Handle("GET /api/scenarios/{id}/activities/{aid}", read(h.getValues))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/driver-values", read(h.putDriverValues))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/amounts", read(h.putAmounts))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/condition", read(h.putCondition))
	mux.Handle("GET /api/scenarios/{id}/activities/{aid}/note", read(h.getNote))
	mux.Handle("PUT /api/scenarios/{id}/activities/{aid}/note", read(h.putNote))
	mux.Handle("POST /api/scenarios/{id}/activities/{aid}/complete", read(h.complete))
	mux.Handle("DELETE /api/scenarios/{id}/activities/{aid}/complete", read(h.uncomplete))
}

// Scenario はシナリオ。
type Scenario struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	FiscalYear     int     `json:"fiscal_year"`
	PlanRole       *string `json:"plan_role"`      // initial / revised / latest
	ActualThrough  *string `json:"actual_through"` // 決算確定月（YYYY-MM）。この月以前は実績
	IsActive       bool    `json:"is_active"`      // 作成中
	BaseScenarioID *int64  `json:"base_scenario_id"`
	// PreviousScenarioID は前回見込（同じ年度のシナリオ）。比較やホームで「前回締めた見込」として使う
	PreviousScenarioID *int64    `json:"previous_scenario_id"`
	IsLocked           bool      `json:"is_locked"`
	CreatedBy          int64     `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// PlanMonths は計画値の月（決算確定月より後の月）。数値を入力できるのはこの月だけ。
func (s Scenario) PlanMonths() []string {
	return calc.PlanMonths(s.FiscalYear, derefStr(s.ActualThrough))
}

// ActualMonths は実績の月（決算確定月以前の月）。
func (s Scenario) ActualMonths() []string {
	plan := s.PlanMonths()
	return slices.DeleteFunc(calc.FiscalMonths(s.FiscalYear), func(m string) bool { return slices.Contains(plan, m) })
}

const scenarioSelect = `SELECT id, name, fiscal_year, plan_role, DATE_FORMAT(actual_through, '%Y-%m'), is_active,
	base_scenario_id, previous_scenario_id, is_locked, created_by, created_at, updated_at FROM scenarios`

func scanScenario(row interface{ Scan(...any) error }) (Scenario, error) {
	var s Scenario
	var base, previous sql.NullInt64
	var role, through sql.NullString
	err := row.Scan(&s.ID, &s.Name, &s.FiscalYear, &role, &through, &s.IsActive, &base, &previous, &s.IsLocked, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.BaseScenarioID = dbx.PtrInt64(base)
	s.PreviousScenarioID = dbx.PtrInt64(previous)
	s.PlanRole = ptrString(role)
	s.ActualThrough = ptrString(through)
	return s, err
}

func ptrString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
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
	FiscalYear     int    `json:"fiscal_year"`
	BaseScenarioID *int64 `json:"base_scenario_id"`
	// PreviousScenarioID は前回見込。省略（null）なら複製元を使う
	PreviousScenarioID *int64 `json:"previous_scenario_id"`
	PlanRole           string `json:"plan_role"`      // 空ならエイリアスなし
	ActualThrough      string `json:"actual_through"` // YYYY-MM。空なら未設定（12か月すべて計画値）
	Reason             string `json:"reason"`
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

// validateRoleAndThrough はエイリアスと決算確定月を検証し、DB に保存する値を返す。
func validateRoleAndThrough(v httpx.Validator, fiscalYear int, role, through string) (any, any) {
	role, through = strings.TrimSpace(role), strings.TrimSpace(through)
	if _, ok := planRoles[role]; role != "" && !ok {
		v.Add("plan_role", "エイリアスは initial（期初計画）/ revised（修正計画）/ latest（最新見込）のいずれかを指定してください")
	}
	if through != "" {
		months := calc.FiscalMonths(fiscalYear)
		if !slices.Contains(months, through) {
			v.Add("actual_through", fmt.Sprintf("決算確定月は %s〜%s の YYYY-MM で指定してください", months[0], months[len(months)-1]))
		}
	}
	return dbx.NullString(role), dbx.NullString(monthDate(through))
}

func monthDate(ym string) string {
	if ym == "" {
		return ""
	}
	return ym + "-01"
}

// takeRole は、同じ年度で role のエイリアスを持つほかのシナリオからエイリアスを外す（付け替え）。
func takeRole(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, fiscalYear int, role string, exceptID int64) error {
	if role == "" {
		return nil
	}
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM scenarios WHERE fiscal_year = ? AND plan_role = ? AND id <> ? FOR UPDATE", fiscalYear, role, exceptID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	before, err := findScenario(ctx, tx, id, "")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET plan_role = NULL WHERE id = ?", id); err != nil {
		return err
	}
	after, err := findScenario(ctx, tx, id, "")
	if err != nil {
		return err
	}
	return rec.Update(ctx, "scenarios", id, before, after)
}

// list は GET /api/scenarios。fiscal_year で絞り込める。新しい年度・新しい順に返す。
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

// active は GET /api/scenarios/active。作成中のシナリオを返す（未設定なら null）。
func (h *Handler) active(w http.ResponseWriter, r *http.Request) error {
	s, err := scanScenario(h.db.QueryRowContext(r.Context(), scenarioSelect+" WHERE is_active"))
	if errors.Is(err, sql.ErrNoRows) {
		// 未設定は null を返す（WriteJSON は nil のとき本文を書かないため、直接書く）
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, err := w.Write([]byte("null\n"))
		return err
	}
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

// create は POST /api/scenarios。base_scenario_id を指定すると、そのシナリオの数値
// （ドライバー値・金額・想定条件）をすべて複製する。複製元は同じ年度のシナリオに限る。
// plan_role を指定すると、同じ年度でそのエイリアスを持つシナリオからは外れる。
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
	if req.FiscalYear < 2000 || req.FiscalYear > 2100 {
		v.Add("fiscal_year", "年度を正しく入力してください（例: 2026）")
	}
	role, through := validateRoleAndThrough(v, req.FiscalYear, req.PlanRole, req.ActualThrough)
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
		if err := takeRole(ctx, tx, rec, req.FiscalYear, strings.TrimSpace(req.PlanRole), 0); err != nil {
			return err
		}
		previous := req.PreviousScenarioID
		if previous == nil {
			previous = req.BaseScenarioID
		}
		if err := checkPrevious(ctx, tx, 0, req.FiscalYear, previous); err != nil {
			return err
		}

		res, err := tx.ExecContext(ctx,
			"INSERT INTO scenarios (name, fiscal_year, plan_role, actual_through, base_scenario_id, previous_scenario_id, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)",
			name, req.FiscalYear, role, through, dbx.NullInt64(req.BaseScenarioID), dbx.NullInt64(previous), u.ID,
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
				{"budget_facts", "activity_id, subject_id, line_id, target_month, amount, source, is_provisional, provisional_reason"},
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

// update は PUT /api/scenarios/{id}。名称・エイリアス・決算確定月を変更する（年度は変更できない）。
// 決算確定月の変更は表示する金額が変わるため変更理由が必須で、ロック済みのシナリオでは変更できない。
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
		Name               string `json:"name"`
		PlanRole           string `json:"plan_role"`
		ActualThrough      string `json:"actual_through"`
		PreviousScenarioID *int64 `json:"previous_scenario_id"` // null なら前回見込なし
		Reason             string `json:"reason"`
	}
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}

	ctx := r.Context()
	var updated Scenario
	err = audit.InTx(ctx, h.db, u.ID, &id, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findScenario(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		v := httpx.Validator{}
		name := v.Text("name", "シナリオ名", req.Name, 100)
		role, through := validateRoleAndThrough(v, before.FiscalYear, req.PlanRole, req.ActualThrough)
		if err := v.Err(); err != nil {
			return err
		}
		if strings.TrimSpace(req.ActualThrough) != derefStr(before.ActualThrough) {
			if before.IsLocked {
				return httpx.Conflict("ロックされたシナリオの決算確定月は変更できません")
			}
			if strings.TrimSpace(req.Reason) == "" {
				return httpx.Validation(map[string]string{"reason": "決算確定月の変更には変更理由の入力が必要です"})
			}
		}
		if err := checkPrevious(ctx, tx, id, before.FiscalYear, req.PreviousScenarioID); err != nil {
			return err
		}
		if err := takeRole(ctx, tx, rec, before.FiscalYear, strings.TrimSpace(req.PlanRole), id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE scenarios SET name = ?, plan_role = ?, actual_through = ?, previous_scenario_id = ? WHERE id = ?",
			name, role, through, dbx.NullInt64(req.PreviousScenarioID), id)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"name": "このシナリオ名は既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findScenario(ctx, tx, id, ""); err != nil {
			return err
		}
		if sameScenario(before, updated) {
			return nil
		}
		if err := rec.Update(ctx, "scenarios", id, before, updated); err != nil {
			return err
		}
		// 計画値の月が変わったら、計算式の金額を再計算する
		if derefStr(before.ActualThrough) != derefStr(updated.ActualThrough) {
			return calc.RecalculateScenario(ctx, tx, rec, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// sameScenario は変更できる項目が同じかを返す。
func sameScenario(a, b Scenario) bool {
	return a.Name == b.Name && derefStr(a.PlanRole) == derefStr(b.PlanRole) && derefStr(a.ActualThrough) == derefStr(b.ActualThrough) &&
		derefID(a.PreviousScenarioID) == derefID(b.PreviousScenarioID)
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// checkPrevious は前回見込が、自分以外の同じ年度のシナリオであることを確認する。self は更新対象（作成時は 0）。
func checkPrevious(ctx context.Context, tx *sql.Tx, self int64, fiscalYear int, previous *int64) error {
	if previous == nil {
		return nil
	}
	if *previous == self {
		return httpx.Validation(map[string]string{"previous_scenario_id": "前回見込に自分自身は選べません"})
	}
	p, err := findScenario(ctx, tx, *previous, "")
	if httpx.IsNotFound(err) {
		return httpx.Validation(map[string]string{"previous_scenario_id": "前回見込のシナリオが見つかりません"})
	}
	if err != nil {
		return err
	}
	if p.FiscalYear != fiscalYear {
		return httpx.Validation(map[string]string{"previous_scenario_id": "前回見込は同じ年度のシナリオを選んでください"})
	}
	return nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// activate は POST /api/scenarios/{id}/activate。作成中のシナリオに指定する（前の作成中は外れる）。
func (h *Handler) activate(w http.ResponseWriter, r *http.Request) error {
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

	ctx := r.Context()
	var updated Scenario
	err = audit.InTx(ctx, h.db, u.ID, &id, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findScenario(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if before.IsLocked {
			return httpx.Conflict("ロックされたシナリオは作成中にできません")
		}
		updated = before
		if before.IsActive {
			return nil
		}
		if err := deactivateCurrent(ctx, tx, rec); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET is_active = TRUE WHERE id = ?", id); err != nil {
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

// deactivateCurrent は作成中のシナリオの指定を外す。
func deactivateCurrent(ctx context.Context, tx *sql.Tx, rec *audit.Recorder) error {
	cur, err := scanScenario(tx.QueryRowContext(ctx, scenarioSelect+" WHERE is_active FOR UPDATE"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET is_active = FALSE WHERE id = ?", cur.ID); err != nil {
		return err
	}
	after := cur
	after.IsActive = false
	return rec.Update(ctx, "scenarios", cur.ID, cur, after)
}

// lock は POST /api/scenarios/{id}/lock。ロックしたシナリオの数値は変更できなくなる。
// 決算確定月以前の実績を scenario_actuals に保存して固定し、作成中の指定は外す。
func (h *Handler) lock(w http.ResponseWriter, r *http.Request) error {
	return h.setLocked(w, r, true)
}

// unlock は POST /api/scenarios/{id}/unlock。確定した版を変更可能に戻すため、変更理由が必須。
// 保存した実績を外し、再び実績データを参照する。
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
		// ロック時は決算確定月以前の実績をシナリオに保存して固定し、解除時は外す（再び実績データを参照する）
		var actuals int64
		if locked && before.ActualThrough != nil {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO scenario_actuals (scenario_id, activity_id, subject_id, target_month, amount)
				SELECT ?, activity_id, subject_id, target_month, amount FROM actual_facts
				WHERE target_month BETWEEN ? AND ?`,
				id, calc.FiscalMonths(before.FiscalYear)[0]+"-01", monthDate(*before.ActualThrough))
			if err != nil {
				return err
			}
			actuals, _ = res.RowsAffected()
		} else if !locked {
			res, err := tx.ExecContext(ctx, "DELETE FROM scenario_actuals WHERE scenario_id = ?", id)
			if err != nil {
				return err
			}
			actuals, _ = res.RowsAffected()
		}
		// 作成中のシナリオをロックすると、作成中の指定は外れる
		if _, err := tx.ExecContext(ctx, "UPDATE scenarios SET is_locked = ?, is_active = is_active AND NOT ? WHERE id = ?", locked, locked, id); err != nil {
			return err
		}
		if updated, err = findScenario(ctx, tx, id, ""); err != nil {
			return err
		}
		// 保存・削除した実績は件数だけを記録する（行ごとの監査ログは actual_facts に残っている）
		return rec.Update(ctx, "scenarios", id, before, struct {
			Scenario
			FrozenActuals int64 `json:"frozen_actuals"`
		}{updated, actuals})
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}
