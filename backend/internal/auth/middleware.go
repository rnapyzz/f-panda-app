package auth

import (
	"context"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type contextKey int

const userContextKey contextKey = 0

// RequireAuth resolves the session cookie to a user and stores it on the
// request context, rejecting the request with 401 when absent/invalid.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.CurrentUser(r.Context(), r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireCSRF enforces the double-submit cookie pattern on mutating
// requests: the SPA must echo the (non-HttpOnly) CSRF cookie value back as
// the X-CSRF-Token header, which a cross-site page cannot read or forge.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "csrf token missing", http.StatusForbidden)
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		if header == "" || header != cookie.Value {
			http.Error(w, "csrf token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFromContext returns the authenticated user attached by RequireAuth.
func UserFromContext(ctx context.Context) (db.AppUser, bool) {
	user, ok := ctx.Value(userContextKey).(db.AppUser)
	return user, ok
}

// RequireRole rejects the request with 403 unless the authenticated user
// (attached earlier by RequireAuth, which must run first in the chain) has
// the given role. Used for office-admin-only actions such as master data
// management, where the business rule ("事務局が固定スキーマを所有する") is
// enforced by the server rather than left to the UI.
func RequireRole(role db.AppUserRole, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if user.Role != role {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
