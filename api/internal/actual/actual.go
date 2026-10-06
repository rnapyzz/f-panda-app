// Package actual は、実績（会計の明細）の取込と、施策への割当の API を提供する（docs/plan.md「2.12 実績の割当」）。
//
// 会計の明細（actual_entries）を取り込み、施策コード・外部コード → 割当ルール（会計科目 × 部門）→ 未割当 の順に
// 施策へ割り当てる。施策 × 科目 × 月の合計（actual_facts）は明細から作り、施策が NULL の行を未割当として持つ。
// アプリの実績の合計は、会計の合計（対象外の会計科目を除く）と常に一致する。
package actual

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"net/http"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// Handler は実績の取込・割当 API のハンドラー。
type Handler struct {
	db *sql.DB
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{db: db}
}

// Register はルートを登録する。requireAdmin は FP&A のみに制限するミドルウェア。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler, requireAdmin func(http.Handler) http.Handler) {
	read := func(f httpx.HandlerFunc) http.Handler { return requireAuth(httpx.Handle(f)) }
	write := func(f httpx.HandlerFunc) http.Handler { return requireAuth(requireAdmin(httpx.Handle(f))) }

	mux.Handle("GET /api/gl-accounts", read(h.listAccounts))
	mux.Handle("POST /api/gl-accounts", write(h.createAccount))
	mux.Handle("GET /api/gl-accounts/export", read(h.exportAccounts))
	mux.Handle("POST /api/gl-accounts/import", write(h.importAccounts))
	mux.Handle("PUT /api/gl-accounts/{id}", write(h.updateAccount))
	mux.Handle("DELETE /api/gl-accounts/{id}", write(h.deleteAccount))

	mux.Handle("GET /api/allocation-rules", read(h.listRules))
	mux.Handle("POST /api/allocation-rules", write(h.createRule))
	mux.Handle("GET /api/allocation-rules/export", read(h.exportRules))
	mux.Handle("POST /api/allocation-rules/import", write(h.importRules))
	mux.Handle("PUT /api/allocation-rules/{id}", write(h.updateRule))
	mux.Handle("DELETE /api/allocation-rules/{id}", write(h.deleteRule))

	mux.Handle("POST /api/actuals/import", write(h.importActuals))
	mux.Handle("GET /api/actuals/months", read(h.actualMonths))
	mux.Handle("GET /api/actuals/unallocated", write(h.listUnallocated))
	mux.Handle("POST /api/actuals/unallocated/assign", write(h.assignUnallocated))
	mux.Handle("POST /api/actuals/reallocate", write(h.reallocate))

	mux.Handle("GET /api/activities/{id}/actual-entries", read(h.activityEntries))

	// 締めた後の実績の修正と年度の締め（docs/plan.md「2.14」）
	mux.Handle("GET /api/scenarios/actual-drift", write(h.listDrift))
	mux.Handle("POST /api/scenarios/{id}/refresh-actuals", write(h.refreshActuals))
	mux.Handle("GET /api/fiscal-years/closings", read(h.listClosings))
	mux.Handle("POST /api/fiscal-years/{fy}/close", write(h.closeYear))
	mux.Handle("POST /api/fiscal-years/{fy}/reopen", write(h.reopenYear))
}

// maxAmount は DECIMAL(18,0) に入る絶対値の上限。
var maxAmount = new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), big.NewInt(1))

// errDryRun は dry run でトランザクションをロールバックさせるためのエラー。
var errDryRun = errors.New("dry run")

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

// inTx はログインユーザーの変更セットを作成し、トランザクション内で fn を実行する。
func inTx(r *http.Request, db *sql.DB, reason string, fn func(tx *sql.Tx, rec *audit.Recorder) error) error {
	u, err := currentUser(r)
	if err != nil {
		return err
	}
	return audit.InTx(r.Context(), db, u.ID, nil, reason, fn)
}

// activityRef は施策のコードと名前。
type activityRef struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func findActivity(ctx context.Context, q dbx.Querier, id int64) (activityRef, error) {
	a := activityRef{ID: id}
	err := q.QueryRowContext(ctx, "SELECT code, name FROM activities WHERE id = ?", id).Scan(&a.Code, &a.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return a, httpx.Validation(map[string]string{"activity_id": "施策が見つかりません"})
	}
	return a, err
}

// placeholders は n 個の "?" をカンマでつないだ文字列を返す。
func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// monthArgs は YYYY-MM の一覧を月初日の引数にする。
func monthArgs(months []string) []any {
	args := make([]any, len(months))
	for i, m := range months {
		args[i] = m + "-01"
	}
	return args
}

func isYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	for i, c := range s {
		if i != 4 && (c < '0' || c > '9') {
			return false
		}
	}
	m := (s[5]-'0')*10 + (s[6] - '0')
	return m >= 1 && m <= 12
}
