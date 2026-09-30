// Package activity は施策と、施策に紐づくマイルストーン・ドライバー定義・計算式の API を提供する。
//
// 権限:
//   - 参照: ログインユーザー全員
//   - 作成・削除: FP&A（全ユニット）、マネージャー（担当者になっているユニットの配下）
//   - 編集: 上記に加え、施策の担当者（manager / member）
//
// 変更は変更セット・監査ログに記録する。確度・前提条件・期間・算出方式の変更、計算式の変更、
// マイルストーン期日の変更、および削除では変更理由（reason）を必須とする。
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
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

// Handler は施策 API のハンドラー。
type Handler struct {
	db *sql.DB
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Register はルートを登録する。権限は各ハンドラーの中で施策・ユニットごとに判定する。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	handle := func(pattern string, f httpx.HandlerFunc) { mux.Handle(pattern, requireAuth(httpx.Handle(f))) }

	handle("GET /api/activities", h.list)
	handle("POST /api/activities", h.create)
	handle("GET /api/activities/{id}", h.get)
	handle("PUT /api/activities/{id}", h.update)
	handle("DELETE /api/activities/{id}", h.delete)

	handle("POST /api/activities/{id}/external-codes", h.createExternalCode)
	handle("DELETE /api/activities/{id}/external-codes/{eid}", h.deleteExternalCode)

	handle("POST /api/activities/{id}/milestones", h.createMilestone)
	handle("PUT /api/activities/{id}/milestones/{mid}", h.updateMilestone)
	handle("DELETE /api/activities/{id}/milestones/{mid}", h.deleteMilestone)

	handle("POST /api/activities/{id}/drivers", h.createDriver)
	handle("PUT /api/activities/{id}/drivers/{did}", h.updateDriver)
	handle("DELETE /api/activities/{id}/drivers/{did}", h.deleteDriver)

	handle("PUT /api/activities/{id}/formulas/{subject_id}", h.putFormula)
	handle("DELETE /api/activities/{id}/formulas/{subject_id}", h.deleteFormula)
}

// 施策タイプ・ステータス・算出方式
var (
	activityTypes = []string{"project", "recurring", "cost_pool"}
	statuses      = []string{"planned", "in_progress", "completed", "on_hold", "cancelled"}
	calcModes     = []string{"manual", "formula"}
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

const (
	maxNameLen        = 200
	maxAssumptionsLen = 5000
	dateLayout        = "2006-01-02"
)

type timestamps struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// activity は施策。
type activity struct {
	ID           int64        `json:"id"`
	UnitID       int64        `json:"unit_id"`
	Code         string       `json:"code"`
	Name         string       `json:"name"`
	ActivityType string       `json:"activity_type"`
	Status       string       `json:"status"`
	StartDate    *string      `json:"start_date"`
	EndDate      *string      `json:"end_date"`
	OwnerUserID  *int64       `json:"owner_user_id"`
	CalcMode     string       `json:"calc_mode"`
	Probability  *json.Number `json:"probability"`
	Assumptions  string       `json:"assumptions"`
	timestamps

	// unitOwnerID は所属するユニットの担当者。権限判定に使う。
	unitOwnerID *int64
}

// activityView はレスポンス用。ログインユーザーが編集できるかを含める。
type activityView struct {
	activity
	CanEdit bool `json:"can_edit"`
}

// activityDetail は施策の詳細（マイルストーン・ドライバー・計算式を含む）。
type activityDetail struct {
	activityView
	ExternalCodes []externalCode `json:"external_codes"`
	Milestones    []milestone    `json:"milestones"`
	Drivers       []driver       `json:"drivers"`
	Formulas      []formulaItem  `json:"formulas"`
}

type activityRequest struct {
	UnitID       int64        `json:"unit_id"`
	Code         string       `json:"code"`
	Name         string       `json:"name"`
	ActivityType string       `json:"activity_type"`
	Status       string       `json:"status"`
	StartDate    *string      `json:"start_date"`
	EndDate      *string      `json:"end_date"`
	OwnerUserID  *int64       `json:"owner_user_id"`
	CalcMode     string       `json:"calc_mode"`
	Probability  *json.Number `json:"probability"`
	Assumptions  string       `json:"assumptions"`
	Reason       string       `json:"reason"`
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

const activitySelect = `
	SELECT a.id, a.unit_id, a.code, a.name, a.activity_type, a.status, a.start_date, a.end_date,
	       a.owner_user_id, a.calc_mode, a.probability, COALESCE(a.assumptions, ''), a.created_at, a.updated_at,
	       un.owner_user_id
	FROM activities a JOIN units un ON un.id = a.unit_id`

func scanActivity(row interface{ Scan(...any) error }) (activity, error) {
	var a activity
	var start, end sql.NullTime
	var owner, fnOwner sql.NullInt64
	var prob sql.NullString
	err := row.Scan(&a.ID, &a.UnitID, &a.Code, &a.Name, &a.ActivityType, &a.Status, &start, &end,
		&owner, &a.CalcMode, &prob, &a.Assumptions, &a.CreatedAt, &a.UpdatedAt, &fnOwner)
	if err != nil {
		return a, err
	}
	a.StartDate = formatDate(start)
	a.EndDate = formatDate(end)
	a.OwnerUserID = dbx.PtrInt64(owner)
	a.unitOwnerID = dbx.PtrInt64(fnOwner)
	if prob.Valid {
		n := json.Number(prob.String)
		a.Probability = &n
	}
	return a, nil
}

func findActivity(ctx context.Context, q dbx.Querier, id int64, lock string) (activity, error) {
	a, err := scanActivity(q.QueryRowContext(ctx, activitySelect+" WHERE a.id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return activity{}, httpx.NotFound("施策が見つかりません")
	}
	return a, err
}

// --- 権限 ---

// canManageUnit はユニットの配下で施策を作成・削除できるかを返す。
func canManageUnit(u auth.User, unitOwnerID *int64) bool {
	switch u.Role {
	case auth.RoleFPAAdmin:
		return true
	case auth.RoleManager:
		return unitOwnerID != nil && *unitOwnerID == u.ID
	}
	return false
}

// canEdit は施策を編集できるかを返す。
func canEdit(u auth.User, a activity) bool {
	if canManageUnit(u, a.unitOwnerID) {
		return true
	}
	isOwner := a.OwnerUserID != nil && *a.OwnerUserID == u.ID
	return isOwner && (u.Role == auth.RoleManager || u.Role == auth.RoleMember)
}

func currentUser(r *http.Request) (auth.User, error) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return auth.User{}, httpx.Unauthorized("ログインしてください")
	}
	return u, nil
}

// lockEditable は施策を行ロック付きで取得し、編集権限を確認する。
func lockEditable(ctx context.Context, tx *sql.Tx, u auth.User, id int64) (activity, error) {
	a, err := findActivity(ctx, tx, id, " FOR UPDATE")
	if err != nil {
		return activity{}, err
	}
	if !canEdit(u, a) {
		return activity{}, httpx.Forbidden()
	}
	return a, nil
}

// lockUnit はユニットを行ロック付きで取得し、担当者を返す。
func lockUnit(ctx context.Context, tx *sql.Tx, id int64) (owner *int64, err error) {
	var o sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT owner_user_id FROM units WHERE id = ? FOR SHARE", id).Scan(&o)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Validation(map[string]string{"unit_id": "ユニットが見つかりません"})
	}
	return dbx.PtrInt64(o), err
}

// Summary は他のパッケージ（シナリオの値入力など）が使う施策の要約。
type Summary struct {
	ID          int64        `json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	CalcMode    string       `json:"calc_mode"`
	Probability *json.Number `json:"probability"`
	CanEdit     bool         `json:"can_edit"`
}

func summarize(u auth.User, a activity) Summary {
	return Summary{ID: a.ID, Code: a.Code, Name: a.Name, CalcMode: a.CalcMode, Probability: a.Probability, CanEdit: canEdit(u, a)}
}

// Load は施策の要約を返す。存在しなければ 404。
func Load(ctx context.Context, q dbx.Querier, u auth.User, id int64) (Summary, error) {
	a, err := findActivity(ctx, q, id, "")
	if err != nil {
		return Summary{}, err
	}
	return summarize(u, a), nil
}

// LockForEdit は施策を行ロック付きで取得し、編集権限がなければ 403 を返す。
func LockForEdit(ctx context.Context, tx *sql.Tx, u auth.User, id int64) (Summary, error) {
	a, err := lockEditable(ctx, tx, u, id)
	if err != nil {
		return Summary{}, err
	}
	return summarize(u, a), nil
}

// RequireReason は変更理由が空ならエラーを返す。
func RequireReason(reason string) error { return requireReason(reason) }

// requireReason は変更理由が空ならエラーを返す。
func requireReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return httpx.Validation(map[string]string{"reason": "この変更には変更理由の入力が必要です"})
	}
	return nil
}

// querier は *sql.DB と *sql.Tx に共通する複数行取得のメソッド。
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func inTx(r *http.Request, db *sql.DB, u auth.User, reason string, fn func(tx *sql.Tx, rec *audit.Recorder) error) error {
	return audit.InTx(r.Context(), db, u.ID, nil, reason, fn)
}

// --- 施策 ---

// list は GET /api/activities。
// クエリパラメーター unit_id / owner_user_id / activity_type / status / q（コード・名称の部分一致）で絞り込める。
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var where []string
	var args []any
	for _, f := range []string{"unit_id", "owner_user_id"} {
		if s := q.Get(f); s != "" {
			id, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return httpx.BadRequest(f + " は数値で指定してください")
			}
			where = append(where, "a."+f+" = ?")
			args = append(args, id)
		}
	}
	for _, f := range []string{"activity_type", "status"} {
		if s := q.Get(f); s != "" {
			where = append(where, "a."+f+" = ?")
			args = append(args, s)
		}
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		like := "%" + escapeLike(s) + "%"
		where = append(where, "(a.code LIKE ? OR a.name LIKE ? OR EXISTS (SELECT 1 FROM activity_external_codes e WHERE e.activity_id = a.id AND e.code LIKE ?))")
		args = append(args, like, like, like)
	}
	query := activitySelect
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY a.code"

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []activityView
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return err
		}
		items = append(items, activityView{activity: a, CanEdit: canEdit(u, a)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

// get は GET /api/activities/{id}。マイルストーン・ドライバー・計算式を含めて返す。
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	a, err := findActivity(ctx, h.db, id, "")
	if err != nil {
		return err
	}
	d := activityDetail{activityView: activityView{activity: a, CanEdit: canEdit(u, a)}}
	if d.Milestones, err = listMilestones(ctx, h.db, id); err != nil {
		return err
	}
	if d.Drivers, err = listDrivers(ctx, h.db, id); err != nil {
		return err
	}
	if d.ExternalCodes, err = listExternalCodes(ctx, h.db, id); err != nil {
		return err
	}
	if d.Formulas, err = listFormulas(ctx, h.db, id); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

// create は POST /api/activities。
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	if u.Role != auth.RoleFPAAdmin && u.Role != auth.RoleManager {
		return httpx.Forbidden()
	}
	var req activityRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.CalcMode == "" {
		req.CalcMode = "manual"
	}
	if req.Status == "" {
		req.Status = "planned"
	}
	// 施策コードが空なら自動で採番する
	autoCode := strings.TrimSpace(req.Code) == ""
	in, err := validateActivity(req, autoCode)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var created activity
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		fnOwner, err := lockUnit(ctx, tx, in.UnitID)
		if err != nil {
			return err
		}
		if !canManageUnit(u, fnOwner) {
			return httpx.Forbidden()
		}
		if err := checkOwner(ctx, tx, in.OwnerUserID); err != nil {
			return err
		}
		insert := func() (sql.Result, error) {
			return tx.ExecContext(ctx, `
				INSERT INTO activities (unit_id, code, name, activity_type, status, start_date, end_date,
				                        owner_user_id, calc_mode, probability, assumptions)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				in.UnitID, in.Code, in.Name, in.ActivityType, in.Status, in.StartDate, in.EndDate,
				dbx.NullInt64(in.OwnerUserID), in.CalcMode, in.Probability, dbx.NullString(in.Assumptions),
			)
		}
		var res sql.Result
		if autoCode {
			// 同時に採番された場合に備えて、重複したら次の番号で再試行する
			for attempt := 0; ; attempt++ {
				if in.Code, err = nextActivityCode(ctx, tx); err != nil {
					return err
				}
				res, err = insert()
				if dbx.ErrNo(err) != dbx.ErrDuplicateEntry || attempt >= 4 {
					break
				}
			}
		} else {
			if err := checkCodeNotExternal(ctx, tx, in.Code); err != nil {
				return err
			}
			res, err = insert()
		}
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "この施策コードは既に使われています"})
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if created, err = findActivity(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "activities", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, activityView{activity: created, CanEdit: canEdit(u, created)})
	return nil
}

// update は PUT /api/activities/{id}。
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req activityRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	in, err := validateActivity(req, false)
	if err != nil {
		return err
	}

	ctx := r.Context()
	var updated activity
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := lockEditable(ctx, tx, u, id)
		if err != nil {
			return err
		}
		// 別のユニットへ移す場合は、移動先のユニットで施策を作成できる権限が必要。
		if in.UnitID != before.UnitID {
			fnOwner, err := lockUnit(ctx, tx, in.UnitID)
			if err != nil {
				return err
			}
			if !canManageUnit(u, fnOwner) {
				return httpx.Forbidden()
			}
		}
		if in.Code != before.Code {
			if err := checkCodeNotExternal(ctx, tx, in.Code); err != nil {
				return err
			}
		}
		if needsReason(before, in) {
			if err := requireReason(req.Reason); err != nil {
				return err
			}
		}
		if err := checkOwner(ctx, tx, in.OwnerUserID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE activities SET unit_id = ?, code = ?, name = ?, activity_type = ?, status = ?,
			       start_date = ?, end_date = ?, owner_user_id = ?, calc_mode = ?, probability = ?, assumptions = ?
			WHERE id = ?`,
			in.UnitID, in.Code, in.Name, in.ActivityType, in.Status, in.StartDate, in.EndDate,
			dbx.NullInt64(in.OwnerUserID), in.CalcMode, in.Probability, dbx.NullString(in.Assumptions), id,
		)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return httpx.Validation(map[string]string{"code": "この施策コードは既に使われています"})
		}
		if err != nil {
			return err
		}
		if updated, err = findActivity(ctx, tx, id, ""); err != nil {
			return err
		}
		if err := rec.Update(ctx, "activities", id, before, updated); err != nil {
			return err
		}
		// 確度・算出方式が変わると計算式で算出した金額も変わる。
		if !sameProbability(before.Probability, in.Probability) || before.CalcMode != in.CalcMode {
			return calc.RecalculateActivity(ctx, tx, rec, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, activityView{activity: updated, CanEdit: canEdit(u, updated)})
	return nil
}

// delete は DELETE /api/activities/{id}。
// 金額・ドライバー値・シナリオ条件が登録済みの施策は、履歴を失わないよう削除できない。
// 外部コード・マイルストーン・ドライバー定義・計算式は合わせて削除する。
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
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
	if err := requireReason(req.Reason); err != nil {
		return err
	}

	ctx := r.Context()
	err = inTx(r, h.db, u, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findActivity(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if !canManageUnit(u, before.unitOwnerID) {
			return httpx.Forbidden()
		}
		for _, check := range []struct{ query, msg string }{
			{"SELECT COUNT(*) FROM budget_facts WHERE activity_id = ?", "金額データが登録されているため削除できません"},
			{"SELECT COUNT(*) FROM driver_values v JOIN activity_drivers d ON d.id = v.activity_driver_id WHERE d.activity_id = ?", "ドライバー値が登録されているため削除できません"},
			{"SELECT COUNT(*) FROM scenario_conditions WHERE activity_id = ?", "シナリオの想定条件が登録されているため削除できません"},
		} {
			n, err := dbx.Count(ctx, tx, check.query, id)
			if err != nil {
				return err
			}
			if n > 0 {
				return httpx.Conflict(check.msg)
			}
		}

		externals, err := listExternalCodes(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, e := range externals {
			if err := rec.Delete(ctx, "activity_external_codes", e.ID, e); err != nil {
				return err
			}
		}
		formulas, err := listFormulas(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, f := range formulas {
			if err := rec.Delete(ctx, "activity_formulas", f.ID, f); err != nil {
				return err
			}
		}
		drivers, err := listDrivers(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, d := range drivers {
			if err := rec.Delete(ctx, "activity_drivers", d.ID, d); err != nil {
				return err
			}
		}
		milestones, err := listMilestones(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, m := range milestones {
			if err := rec.Delete(ctx, "activity_milestones", m.ID, m); err != nil {
				return err
			}
		}
		for _, table := range []string{"activity_external_codes", "activity_formulas", "activity_drivers", "activity_milestones"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE activity_id = ?", id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activities WHERE id = ?", id); err != nil {
			return err
		}
		return rec.Delete(ctx, "activities", id, before)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// activityInput は検証・正規化済みの入力。
type activityInput struct {
	UnitID       int64
	Code         string
	Name         string
	ActivityType string
	Status       string
	StartDate    sql.NullString
	EndDate      sql.NullString
	OwnerUserID  *int64
	CalcMode     string
	Probability  sql.NullString // DECIMAL(5,4) の文字列表現
	Assumptions  string
}

// validateActivity は施策の入力を検証する。allowEmptyCode なら施策コードの空欄を許す（自動採番する）。
func validateActivity(req activityRequest, allowEmptyCode bool) (activityInput, error) {
	v := httpx.Validator{}
	in := activityInput{
		UnitID:       req.UnitID,
		Code:         strings.TrimSpace(req.Code),
		Name:         v.Text("name", "施策名", req.Name, maxNameLen),
		ActivityType: req.ActivityType,
		Status:       req.Status,
		OwnerUserID:  req.OwnerUserID,
		CalcMode:     req.CalcMode,
		Assumptions:  v.OptionalText("assumptions", "前提条件", req.Assumptions, maxAssumptionsLen),
	}
	if in.UnitID <= 0 {
		v.Add("unit_id", "ユニットを選択してください")
	}
	if !(allowEmptyCode && in.Code == "") && !codePattern.MatchString(in.Code) {
		v.Add("code", "施策コードは半角英数字・ハイフン・アンダースコアの50文字以内で入力してください")
	}
	if !slices.Contains(activityTypes, in.ActivityType) {
		v.Add("activity_type", "施策タイプは project / recurring / cost_pool のいずれかを指定してください")
	}
	if !slices.Contains(statuses, in.Status) {
		v.Add("status", "ステータスは "+strings.Join(statuses, " / ")+" のいずれかを指定してください")
	}
	if !slices.Contains(calcModes, in.CalcMode) {
		v.Add("calc_mode", "算出方式は manual / formula のいずれかを指定してください")
	}

	start, startOK := parseDate(v, "start_date", "開始日", req.StartDate)
	end, endOK := parseDate(v, "end_date", "終了日", req.EndDate)
	in.StartDate, in.EndDate = start, end
	if startOK && endOK && start.Valid && end.Valid && start.String > end.String {
		v.Add("end_date", "終了日は開始日以降にしてください")
	}
	if in.ActivityType == "project" {
		if !start.Valid && startOK {
			v.Add("start_date", "プロジェクト型の施策は開始日を入力してください")
		}
		if !end.Valid && endOK {
			v.Add("end_date", "プロジェクト型の施策は終了日を入力してください")
		}
	}

	if req.Probability != nil {
		p, ok := new(big.Rat).SetString(string(*req.Probability))
		switch {
		case !ok:
			v.Add("probability", "確度は0〜1の数値で入力してください")
		case p.Sign() < 0 || p.Cmp(big.NewRat(1, 1)) > 0:
			v.Add("probability", "確度は0〜1の範囲で入力してください")
		case !new(big.Rat).Mul(p, big.NewRat(10000, 1)).IsInt():
			v.Add("probability", "確度は小数点以下4桁までで入力してください")
		default:
			in.Probability = sql.NullString{String: p.FloatString(4), Valid: true}
		}
	}
	return in, v.Err()
}

// needsReason は変更理由が必須になる項目（確度・前提条件・期間・算出方式）が変わるかを返す。
func needsReason(before activity, in activityInput) bool {
	return !sameProbability(before.Probability, in.Probability) ||
		before.Assumptions != in.Assumptions ||
		!sameDate(before.StartDate, in.StartDate) ||
		!sameDate(before.EndDate, in.EndDate) ||
		before.CalcMode != in.CalcMode
}

func sameProbability(before *json.Number, after sql.NullString) bool {
	if before == nil || !after.Valid {
		return before == nil && !after.Valid
	}
	a, _ := new(big.Rat).SetString(string(*before))
	b, _ := new(big.Rat).SetString(after.String)
	return a != nil && b != nil && a.Cmp(b) == 0
}

func sameDate(before *string, after sql.NullString) bool {
	if before == nil || !after.Valid {
		return before == nil && !after.Valid
	}
	return *before == after.String
}

func checkOwner(ctx context.Context, tx *sql.Tx, ownerID *int64) error {
	if ownerID == nil {
		return nil
	}
	ok, err := dbx.Exists(ctx, tx, "users", *ownerID)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Validation(map[string]string{"owner_user_id": "担当者が見つかりません"})
	}
	return nil
}

// parseDate は YYYY-MM-DD の日付を検証する。ok は形式が正しい（または未指定）かどうか。
func parseDate(v httpx.Validator, field, label string, s *string) (sql.NullString, bool) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return sql.NullString{}, true
	}
	t, err := time.Parse(dateLayout, strings.TrimSpace(*s))
	if err != nil {
		v.Add(field, label+"は YYYY-MM-DD 形式で入力してください")
		return sql.NullString{}, false
	}
	return sql.NullString{String: t.Format(dateLayout), Valid: true}, true
}

func formatDate(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(dateLayout)
	return &s
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
