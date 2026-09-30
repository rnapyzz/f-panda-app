package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// CookieName はセッション Cookie の名前。
const CookieName = "fpanda_session"

type ctxKey struct{}

// WithUser は ctx にログインユーザーを設定する。
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom は ctx からログインユーザーを取り出す。RequireAuth を通ったハンドラーでは必ず存在する。
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// RequireAuth はログイン済みでなければ 401 を返すミドルウェア。
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.Authenticate(r.Context(), sessionToken(r))
		if errors.Is(err, ErrNoSession) {
			httpx.WriteError(w, r, httpx.Unauthorized("ログインしてください"))
			return
		}
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// RequireRole はログインユーザーが roles のいずれかでなければ 403 を返すミドルウェア。
// RequireAuth の内側で使う。
func RequireRole(roles ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFrom(r.Context())
			if !ok {
				httpx.WriteError(w, r, httpx.Unauthorized("ログインしてください"))
				return
			}
			if !slices.Contains(roles, u.Role) {
				httpx.WriteError(w, r, httpx.Forbidden())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
