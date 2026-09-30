// Package master はマスタ（組織・セグメント・機能・勘定科目・ユーザー）の管理 API を提供する。
//
// 参照はログインユーザー全員、更新は FP&A（fpa_admin）のみが行える。
// 更新はすべて変更セットと監査ログに記録する。変更理由（reason）は任意。
package master

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/api/internal/audit"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
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

// inTx はトランザクション内で fn を実行し、変更セットを作成して渡す。fn がエラーを返したらロールバックする。
func inTx(ctx context.Context, db *sql.DB, r *http.Request, reason string, fn func(tx *sql.Tx, rec *audit.Recorder) error) error {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return httpx.Unauthorized("ログインしてください")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rec, err := audit.Begin(ctx, tx, u.ID, nil, reason)
	if err != nil {
		return err
	}
	if err := fn(tx, rec); err != nil {
		return err
	}
	return tx.Commit()
}

// MySQL のエラー番号
const (
	errDuplicateEntry  = 1062
	errRowIsReferenced = 1451
)

func mysqlErrNo(err error) uint16 {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number
	}
	return 0
}

// deleteError は削除時の MySQL エラーを API エラーに変換する。
func deleteError(err error, label string) error {
	if mysqlErrNo(err) == errRowIsReferenced {
		return httpx.Conflict(label + "は他のデータから参照されているため削除できません")
	}
	return err
}

// validator は入力値の検証エラーを集める。
type validator map[string]string

func (v validator) add(field, msg string) {
	if _, ok := v[field]; !ok {
		v[field] = msg
	}
}

// text は必須の文字列を検証し、前後の空白を除いた値を返す。
func (v validator) text(field, label, s string, maxLen int) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		v.add(field, label+"を入力してください")
	case utf8.RuneCountInString(s) > maxLen:
		v.add(field, fmt.Sprintf("%sは%d文字以下にしてください", label, maxLen))
	}
	return s
}

func (v validator) err() error {
	if len(v) == 0 {
		return nil
	}
	return httpx.Validation(v)
}

// exists は table に id のレコードが存在するかを返す。tx 内で FOR SHARE ロックを取る。
func exists(ctx context.Context, tx *sql.Tx, table string, id int64) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id = ? FOR SHARE", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// count は query の結果（COUNT(*)）を返す。
func count(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

// timestamps はレスポンスに含める作成・更新日時。
type timestamps struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func nullInt64(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func ptrInt64(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func notFound(label string) error {
	return httpx.NotFound(label + "が見つかりません")
}

func writeItem(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}

func writeList[T any](w http.ResponseWriter, items []T) {
	if items == nil {
		items = []T{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]T{"items": items})
}
