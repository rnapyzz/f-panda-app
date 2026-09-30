// Package master はマスタ（組織・セグメント・ユニット・勘定科目・ユーザー）の管理 API を提供する。
//
// 参照はログインユーザー全員、更新は FP&A（fpa_admin）のみが行える。
// 更新はすべて変更セットと監査ログに記録する。変更理由（reason）は任意。
package master

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// Handler はマスタ管理 API のハンドラー。
type Handler struct {
	db            *sql.DB
	organizations *treeHandler
	segments      *treeHandler
}

// NewHandler は Handler を作る。
func NewHandler(db *sql.DB) *Handler {
	return &Handler{
		db:            db,
		organizations: &treeHandler{db: db, table: "organizations", label: "組織", functionColumn: "organization_id"},
		segments:      &treeHandler{db: db, table: "segments", label: "セグメント", functionColumn: "segment_id"},
	}
}

// Register はルートを登録する。requireAuth はログイン必須、requireAdmin は FP&A のみに制限するミドルウェア。
func (h *Handler) Register(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler, requireAdmin func(http.Handler) http.Handler) {
	read := func(f httpx.HandlerFunc) http.Handler { return requireAuth(httpx.Handle(f)) }
	write := func(f httpx.HandlerFunc) http.Handler { return requireAuth(requireAdmin(httpx.Handle(f))) }

	for path, t := range map[string]*treeHandler{"organizations": h.organizations, "segments": h.segments} {
		mux.Handle("GET /api/"+path, read(t.list))
		mux.Handle("POST /api/"+path, write(t.create))
		mux.Handle("GET /api/"+path+"/{id}", read(t.get))
		mux.Handle("PUT /api/"+path+"/{id}", write(t.update))
		mux.Handle("DELETE /api/"+path+"/{id}", write(t.delete))
	}

	mux.Handle("GET /api/functions", read(h.listFunctions))
	mux.Handle("POST /api/functions", write(h.createFunction))
	mux.Handle("GET /api/functions/{id}", read(h.getFunction))
	mux.Handle("PUT /api/functions/{id}", write(h.updateFunction))
	mux.Handle("DELETE /api/functions/{id}", write(h.deleteFunction))

	mux.Handle("GET /api/subjects", read(h.listSubjects))
	mux.Handle("POST /api/subjects", write(h.createSubject))
	mux.Handle("GET /api/subjects/{id}", read(h.getSubject))
	mux.Handle("PUT /api/subjects/{id}", write(h.updateSubject))
	mux.Handle("DELETE /api/subjects/{id}", write(h.deleteSubject))

	mux.Handle("GET /api/users", read(h.listUsers))
	mux.Handle("POST /api/users", write(h.createUser))
	mux.Handle("GET /api/users/{id}", read(h.getUser))
	mux.Handle("PUT /api/users/{id}", write(h.updateUser))
	mux.Handle("PUT /api/users/{id}/password", write(h.resetPassword))
}

// reasonRequest は更新系リクエストに共通する変更理由。
type reasonRequest struct {
	Reason string `json:"reason"`
}

// inTx はログインユーザーの変更セットを作成し、トランザクション内で fn を実行する。
func inTx(ctx context.Context, db *sql.DB, r *http.Request, reason string, fn func(tx *sql.Tx, rec *audit.Recorder) error) error {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("ログインしてください")
	}
	return audit.InTx(ctx, db, u.ID, nil, reason, fn)
}

// deleteError は削除時の MySQL エラーを API エラーに変換する。
func deleteError(err error, label string) error {
	if dbx.ErrNo(err) == dbx.ErrRowIsReferenced {
		return httpx.Conflict(label + "は他のデータから参照されているため削除できません")
	}
	return err
}

// timestamps はレスポンスに含める作成・更新日時。
type timestamps struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func notFound(label string) error {
	return httpx.NotFound(label + "が見つかりません")
}
