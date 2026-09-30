// Package server は API のルーティングと共通ミドルウェアを組み立てる。
package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/activity"
	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/history"
	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
	"github.com/rnapyzz/f-panda-app/api/internal/master"
	"github.com/rnapyzz/f-panda-app/api/internal/report"
	"github.com/rnapyzz/f-panda-app/api/internal/scenario"
)

// Deps はハンドラーが依存するもの。
type Deps struct {
	DB           *sql.DB
	Auth         *auth.Service
	CookieSecure bool
	Logger       *slog.Logger
}

// NewHandler は API 全体の http.Handler を返す。
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()

	authH := auth.NewHandler(d.Auth, d.CookieSecure)
	requireAuth := d.Auth.RequireAuth

	mux.HandleFunc("GET /api/health", healthHandler(d.DB))
	mux.HandleFunc("POST /api/auth/login", httpx.Handle(authH.Login))
	mux.HandleFunc("POST /api/auth/logout", httpx.Handle(authH.Logout))
	mux.Handle("GET /api/auth/me", requireAuth(httpx.Handle(authH.Me)))

	master.NewHandler(d.DB).Register(mux, requireAuth, auth.RequireRole(auth.RoleFPAAdmin))
	activity.NewHandler(d.DB).Register(mux, requireAuth)
	scenario.NewHandler(d.DB).Register(mux, requireAuth, auth.RequireRole(auth.RoleFPAAdmin))
	report.NewHandler(d.DB).Register(mux, requireAuth)
	history.NewHandler(d.DB).Register(mux, requireAuth)

	// 未定義の /api パスは JSON で 404 を返す。
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, httpx.NotFound("API が見つかりません"))
	})

	// CSRF 対策: 別オリジンからのブラウザによる更新系リクエストを拒否する。
	csrf := http.NewCrossOriginProtection()
	return recoverer(d.Logger, accessLog(d.Logger, csrf.Handler(mux)))
}

func healthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		status := http.StatusOK
		body := map[string]string{"status": "ok", "database": "ok"}
		if err := db.PingContext(ctx); err != nil {
			status = http.StatusServiceUnavailable
			body["status"] = "degraded"
			body["database"] = "unavailable"
		}
		httpx.WriteJSON(w, status, body)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", v)
				httpx.WriteError(w, r, &httpx.Error{Status: http.StatusInternalServerError, Code: "internal_error", Message: "サーバーでエラーが発生しました"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
