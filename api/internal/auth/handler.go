package auth

import (
	"errors"
	"net/http"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// Handler は認証 API のハンドラー。
type Handler struct {
	svc          *Service
	secureCookie bool
}

// NewHandler は Handler を作る。secureCookie が true のとき Cookie に Secure 属性を付ける（HTTPS 環境用）。
func NewHandler(svc *Service, secureCookie bool) *Handler {
	return &Handler{svc: svc, secureCookie: secureCookie}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login は POST /api/auth/login。
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	details := map[string]string{}
	if req.Email == "" {
		details["email"] = "メールアドレスを入力してください"
	}
	if req.Password == "" {
		details["password"] = "パスワードを入力してください"
	}
	if len(details) > 0 {
		return httpx.Validation(details)
	}

	token, u, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		return httpx.Unauthorized("メールアドレスまたはパスワードが正しくありません")
	}
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.svc.TTL().Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]User{"user": u})
	return nil
}

// Logout は POST /api/auth/logout。
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	if err := h.svc.Logout(r.Context(), sessionToken(r)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Me は GET /api/auth/me。RequireAuth の内側で使う。
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	u, _ := UserFrom(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]User{"user": u})
	return nil
}
