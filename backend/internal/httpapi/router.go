// Package httpapi wires the application's HTTP routes and middleware chain
// on top of the standard library's net/http.ServeMux (Go 1.22+ method+path
// patterns), deliberately without a third-party router framework.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
)

func NewRouter(authSvc *auth.Service, spaHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)

	// The login endpoint is deliberately exempt from CSRF checking: the CSRF
	// cookie doesn't exist until a session does, so requiring it here would
	// make it impossible to ever log in. Once authenticated, every mutating
	// route goes through both RequireAuth and RequireCSRF.
	mux.HandleFunc("POST /api/auth/login", authSvc.LoginHandler)
	mux.Handle("POST /api/auth/logout", authSvc.RequireAuth(auth.RequireCSRF(http.HandlerFunc(authSvc.LogoutHandler))))
	mux.Handle("GET /api/auth/me", authSvc.RequireAuth(http.HandlerFunc(authSvc.MeHandler)))

	if spaHandler != nil {
		mux.Handle("/", spaHandler)
	}

	return withMiddleware(mux)
}

func withMiddleware(h http.Handler) http.Handler {
	return recoverMiddleware(logMiddleware(h))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
