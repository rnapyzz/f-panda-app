package orgchange

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const maxItems = 500

// Options は Service の設定。
type Options struct {
	Logger *slog.Logger
	// Now はテスト用の時計。nil なら time.Now
	Now func() time.Time
}

// Service は組織変更の API と、予約の自動の適用を行う。
type Service struct {
	db     *sql.DB
	logger *slog.Logger
	now    func() time.Time
}

// NewService は Service を作る。
func NewService(db *sql.DB, o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{db: db, logger: o.Logger, now: o.Now}
}

var jst = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("Asia/Tokyo", 9*60*60)
	}
	return loc
}()

func (s *Service) today() string { return s.now().In(jst).Format("2006-01-02") }

// Register はルートを登録する。requireAdmin は FP&A のみに制限するミドルウェア。
func (s *Service) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler, requireAdmin func(http.Handler) http.Handler) {
	write := func(f httpx.HandlerFunc) http.Handler { return requireAuth(requireAdmin(httpx.Handle(f))) }

	mux.Handle("GET /api/org-change-plans", write(s.listPlans))
	mux.Handle("POST /api/org-change-plans", write(s.createPlan))
	mux.Handle("GET /api/org-change-plans/{id}", write(s.getPlan))
	mux.Handle("PUT /api/org-change-plans/{id}", write(s.updatePlan))
	mux.Handle("DELETE /api/org-change-plans/{id}", write(s.deletePlan))
	mux.Handle("POST /api/org-change-plans/{id}/apply", write(s.applyNow))
	mux.Handle("POST /api/org-change-plans/{id}/cancel", write(s.cancelPlan))

	mux.Handle("POST /api/units/{id}/merge", write(s.mergeUnit))
	mux.Handle("POST /api/units/{id}/archive", write(s.archiveUnit))
	mux.Handle("POST /api/units/{id}/unarchive", write(s.unarchiveUnit))
	mux.Handle("GET /api/users/{id}/assignments", write(s.assignments))
	mux.Handle("POST /api/users/{id}/deactivate", write(s.deactivate))
}

func currentUser(r *http.Request) (auth.User, error) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return auth.User{}, httpx.Unauthorized("ログインしてください")
	}
	return u, nil
}

// plan は組織変更の予約。
type plan struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	EffectiveDate string     `json:"effective_date"`
	Status        string     `json:"status"`
	CreatedBy     int64      `json:"created_by"`
	CreatedByName string     `json:"created_by_name"`
	AppliedAt     *time.Time `json:"applied_at"`
	Error         string     `json:"error"`
	ItemCount     int        `json:"item_count"`
	Items         []item     `json:"items,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const planSelect = `
	SELECT p.id, p.name, DATE_FORMAT(p.effective_date, '%Y-%m-%d'), p.status, p.created_by, u.name, p.applied_at, COALESCE(p.error, ''),
	       (SELECT COUNT(*) FROM org_change_items i WHERE i.plan_id = p.id), p.created_at, p.updated_at
	FROM org_change_plans p JOIN users u ON u.id = p.created_by`

func scanPlan(row interface{ Scan(...any) error }) (plan, error) {
	var p plan
	var applied sql.NullTime
	err := row.Scan(&p.ID, &p.Name, &p.EffectiveDate, &p.Status, &p.CreatedBy, &p.CreatedByName, &applied, &p.Error, &p.ItemCount, &p.CreatedAt, &p.UpdatedAt)
	if applied.Valid {
		p.AppliedAt = &applied.Time
	}
	return p, err
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func findPlan(ctx context.Context, q querier, id int64, lock string) (plan, error) {
	p, err := scanPlan(q.QueryRowContext(ctx, planSelect+" WHERE p.id = ?"+lock, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, httpx.NotFound("組織変更の予約が見つかりません")
	}
	if err != nil {
		return p, err
	}
	p.Items, err = loadItems(ctx, q, id)
	return p, err
}

func loadItems(ctx context.Context, q querier, planID int64) ([]item, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, kind, activity_id, unit_id, target_unit_id, segment_id, organization_id, owner_user_id
		FROM org_change_items WHERE plan_id = ? ORDER BY sort_order, id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []item{}
	for rows.Next() {
		var it item
		var a, u, t, sg, o, ow sql.NullInt64
		if err := rows.Scan(&it.ID, &it.Kind, &a, &u, &t, &sg, &o, &ow); err != nil {
			return nil, err
		}
		it.ActivityID, it.UnitID, it.TargetUnitID = dbx.PtrInt64(a), dbx.PtrInt64(u), dbx.PtrInt64(t)
		it.SegmentID, it.OrganizationID, it.OwnerUserID = dbx.PtrInt64(sg), dbx.PtrInt64(o), dbx.PtrInt64(ow)
		items = append(items, it)
	}
	return items, rows.Err()
}

// listPlans は GET /api/org-change-plans。有効日の新しい順。
func (s *Service) listPlans(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.db.QueryContext(r.Context(), planSelect+" ORDER BY p.effective_date DESC, p.id DESC")
	if err != nil {
		return err
	}
	defer rows.Close()
	var items []plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteList(w, items)
	return nil
}

func (s *Service) getPlan(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	p, err := findPlan(r.Context(), s.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

type planRequest struct {
	Name          string `json:"name"`
	EffectiveDate string `json:"effective_date"`
	Items         []item `json:"items"`
	Reason        string `json:"reason"`
}

// validatePlan は入力の形を確かめる（対象があるか・適用できるかは validateApplicable で確かめる）。
func (s *Service) validatePlan(req *planRequest) error {
	v := httpx.Validator{}
	req.Name = v.Text("name", "名前", req.Name, 100)
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.EffectiveDate), jst)
	switch {
	case err != nil:
		v.Add("effective_date", "有効日は YYYY-MM-DD の形式で入力してください")
	case d.Format("2006-01-02") <= s.today():
		v.Add("effective_date", "有効日は明日以降の日付にしてください（今日から有効にするには、作ってから「今すぐ適用」します）")
	default:
		req.EffectiveDate = d.Format("2006-01-02")
	}
	switch {
	case len(req.Items) == 0:
		v.Add("items", "変更を1つ以上登録してください")
	case len(req.Items) > maxItems:
		v.Add("items", fmt.Sprintf("変更は %d 件までです", maxItems))
	}
	for i, it := range req.Items {
		if msg := checkShape(it); msg != "" {
			v.Add("items", fmt.Sprintf("%d 件目の変更: %s", i+1, msg))
			break
		}
	}
	return v.Err()
}

// checkShape は変更の種類ごとに必要な項目があるかを確かめる。
func checkShape(it item) string {
	switch it.Kind {
	case KindMoveActivity:
		if it.ActivityID == nil || it.TargetUnitID == nil {
			return "施策と移動先のユニットを選んでください"
		}
	case KindMoveUnit:
		if it.UnitID == nil || it.SegmentID == nil || it.OrganizationID == nil {
			return "ユニットと、所属先のセグメント・組織を選んでください"
		}
	case KindMergeUnit:
		if it.UnitID == nil || it.TargetUnitID == nil {
			return "ユニットと統合先のユニットを選んでください"
		}
	case KindChangeOwner:
		if (it.ActivityID == nil) == (it.UnitID == nil) {
			return "施策かユニットのどちらかを選んでください"
		}
	default:
		return "変更の種類は move_activity / move_unit / merge_unit / change_owner のいずれかです"
	}
	return ""
}

var errValidateOnly = errors.New("validate only")

// validateApplicable は、今の状態で予約を適用できるかを、適用してロールバックして確かめる。
func (s *Service) validateApplicable(ctx context.Context, userID int64, items []item) error {
	err := audit.InTx(ctx, s.db, userID, nil, "", func(tx *sql.Tx, rec *audit.Recorder) error {
		if err := applyItems(ctx, tx, rec, items); err != nil {
			return err
		}
		return errValidateOnly
	})
	var ie *itemError
	if errors.As(err, &ie) {
		return httpx.Validation(map[string]string{"items": ie.Error()})
	}
	if errors.Is(err, errValidateOnly) {
		return nil
	}
	return err
}

func saveItems(ctx context.Context, tx *sql.Tx, planID int64, items []item) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM org_change_items WHERE plan_id = ?", planID); err != nil {
		return err
	}
	for i, it := range items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO org_change_items (plan_id, sort_order, kind, activity_id, unit_id, target_unit_id, segment_id, organization_id, owner_user_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			planID, i, it.Kind, dbx.NullInt64(it.ActivityID), dbx.NullInt64(it.UnitID), dbx.NullInt64(it.TargetUnitID),
			dbx.NullInt64(it.SegmentID), dbx.NullInt64(it.OrganizationID), dbx.NullInt64(it.OwnerUserID)); err != nil {
			return err
		}
	}
	return nil
}

// createPlan は POST /api/org-change-plans。今の状態で適用できない予約は作れない。
func (s *Service) createPlan(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	var req planRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := s.validatePlan(&req); err != nil {
		return err
	}
	ctx := r.Context()
	if err := s.validateApplicable(ctx, u.ID, req.Items); err != nil {
		return err
	}
	var created plan
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		res, err := tx.ExecContext(ctx, "INSERT INTO org_change_plans (name, effective_date, created_by) VALUES (?, ?, ?)", req.Name, req.EffectiveDate, u.ID)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if err := saveItems(ctx, tx, id, req.Items); err != nil {
			return err
		}
		if created, err = findPlan(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Insert(ctx, "org_change_plans", id, created)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// updatePlan は PUT /api/org-change-plans/{id}。予約中・失敗の予約だけを変えられる（変えると予約中に戻る）。
func (s *Service) updatePlan(w http.ResponseWriter, r *http.Request) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	var req planRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := s.validatePlan(&req); err != nil {
		return err
	}
	ctx := r.Context()
	if err := s.validateApplicable(ctx, u.ID, req.Items); err != nil {
		return err
	}
	var updated plan
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		before, err := findPlan(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if !slices.Contains([]string{"scheduled", "failed"}, before.Status) {
			return httpx.Conflict("適用済み・取り消した予約は変更できません")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE org_change_plans SET name = ?, effective_date = ?, status = 'scheduled', error = NULL WHERE id = ?",
			req.Name, req.EffectiveDate, id); err != nil {
			return err
		}
		if err := saveItems(ctx, tx, id, req.Items); err != nil {
			return err
		}
		if updated, err = findPlan(ctx, tx, id, ""); err != nil {
			return err
		}
		return rec.Update(ctx, "org_change_plans", id, before, updated)
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

// deletePlan は DELETE /api/org-change-plans/{id}。予約中・失敗・取り消しの予約を削除する（適用済みは履歴として残す）。
func (s *Service) deletePlan(w http.ResponseWriter, r *http.Request) error {
	return s.changeStatus(w, r, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, p plan) error {
		if p.Status == "applied" {
			return httpx.Conflict("適用済みの予約は削除できません")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM org_change_plans WHERE id = ?", p.ID); err != nil {
			return err
		}
		return rec.Delete(ctx, "org_change_plans", p.ID, p)
	}, http.StatusNoContent)
}

// cancelPlan は POST /api/org-change-plans/{id}/cancel。予約中の予約を取り消す。
func (s *Service) cancelPlan(w http.ResponseWriter, r *http.Request) error {
	return s.changeStatus(w, r, func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, p plan) error {
		if p.Status != "scheduled" && p.Status != "failed" {
			return httpx.Conflict("取り消せるのは、予約中・失敗の予約だけです")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE org_change_plans SET status = 'cancelled' WHERE id = ?", p.ID); err != nil {
			return err
		}
		after := p
		after.Status = "cancelled"
		return rec.Update(ctx, "org_change_plans", p.ID, p, after)
	}, http.StatusOK)
}

func (s *Service) changeStatus(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, tx *sql.Tx, rec *audit.Recorder, p plan) error, status int) error {
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
	err = audit.InTx(ctx, s.db, u.ID, nil, req.Reason, func(tx *sql.Tx, rec *audit.Recorder) error {
		p, err := findPlan(ctx, tx, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		return fn(ctx, tx, rec, p)
	})
	if err != nil {
		return err
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return nil
	}
	p, err := findPlan(ctx, s.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, status, p)
	return nil
}

// applyNow は POST /api/org-change-plans/{id}/apply。予約中・失敗の予約を今すぐ適用する。
// 適用できなければ 422 で理由を返し、予約を失敗にする。
func (s *Service) applyNow(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if err := s.Apply(ctx, id, false); err != nil {
		return err
	}
	p, err := findPlan(ctx, s.db, id, "")
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// Apply は予約を適用する。予約を作った FP&A の変更セットとして記録する。
// 失敗したら全体をロールバックし、予約を失敗にして理由を残し、FP&A 全員にお知らせを送る。
// onlyDue なら、有効日が今日以前の予約中の予約だけを適用する（自動の適用）。
func (s *Service) Apply(ctx context.Context, id int64, onlyDue bool) error {
	var p plan
	err := func() error {
		var err error
		if p, err = findPlan(ctx, s.db, id, ""); err != nil {
			return err
		}
		reason := fmt.Sprintf("組織変更の予約「%s」を適用", p.Name)
		return audit.InTx(ctx, s.db, p.CreatedBy, nil, reason, func(tx *sql.Tx, rec *audit.Recorder) error {
			cur, err := findPlan(ctx, tx, id, " FOR UPDATE") // 二重の適用を防ぐ
			if err != nil {
				return err
			}
			if onlyDue && (cur.Status != "scheduled" || cur.EffectiveDate > s.today()) {
				return errSkip
			}
			if cur.Status != "scheduled" && cur.Status != "failed" {
				return httpx.Conflict("適用できるのは、予約中・失敗の予約だけです")
			}
			if err := applyItems(ctx, tx, rec, cur.Items); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE org_change_plans SET status = 'applied', applied_at = NOW(), error = NULL WHERE id = ?", id); err != nil {
				return err
			}
			after := cur
			after.Status = "applied"
			return rec.Update(ctx, "org_change_plans", id, cur, after)
		})
	}()
	if err == nil || errors.Is(err, errSkip) {
		return nil
	}
	var ie *itemError
	if !errors.As(err, &ie) {
		return err
	}
	if ferr := s.markFailed(ctx, p, ie.Error()); ferr != nil {
		return ferr
	}
	return httpx.Validation(map[string]string{"items": ie.Error()})
}

var errSkip = errors.New("skip")

// markFailed は予約を失敗にし、FP&A 全員にアプリ内のお知らせを送る。
func (s *Service) markFailed(ctx context.Context, p plan, msg string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE org_change_plans SET status = 'failed', error = ? WHERE id = ?", msg, p.ID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (user_id, kind, scenario_id, title, body, link)
		SELECT id, 'org_change_failed', NULL, ?, ?, '/admin/org-changes' FROM users WHERE role = 'fpa_admin' AND is_active`,
		fmt.Sprintf("組織変更の予約「%s」を適用できませんでした", p.Name),
		fmt.Sprintf("有効日 %s の予約を適用できなかったため、何も変更していません。原因を直して「今すぐ適用」してください。\n%s", p.EffectiveDate, msg))
	return err
}

// Run は1分ごとに ApplyDue を呼ぶ。ctx が終わると戻る。
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := s.ApplyDue(ctx); err != nil {
			s.logger.Error("org change: apply due", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ApplyDue は、有効日が今日以前の予約中の予約を、有効日の順に適用する。
func (s *Service) ApplyDue(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM org_change_plans WHERE status = 'scheduled' AND effective_date <= ? ORDER BY effective_date, id", s.today())
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.Apply(ctx, id, true); err != nil {
			var apiErr *httpx.Error
			if errors.As(err, &apiErr) {
				s.logger.Warn("org change: plan failed", "plan_id", id, "error", err)
				continue
			}
			return err
		}
	}
	return nil
}
