package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

// HandlerOptions は認証 API の設定。
type HandlerOptions struct {
	// SecureCookie が true のとき Cookie に Secure 属性を付ける（HTTPS 環境用）
	SecureCookie bool
	// TrustProxy が true のとき、ログイン失敗の制限に使う IP を X-Forwarded-For の最後の値（ALB が付けたもの）から読む
	TrustProxy bool
	// OIDC は SSO。nil なら SSO は無効
	OIDC   *OIDC
	Logger *slog.Logger
}

// Handler は認証 API のハンドラー。
type Handler struct {
	svc          *Service
	secureCookie bool
	trustProxy   bool
	oidc         *OIDC
	logger       *slog.Logger
}

// NewHandler は Handler を作る。
func NewHandler(svc *Service, o HandlerOptions) *Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Handler{svc: svc, secureCookie: o.SecureCookie, trustProxy: o.TrustProxy, oidc: o.OIDC, logger: o.Logger}
}

// clientIP はログイン失敗の制限に使う接続元の IP を返す。
func (h *Handler) clientIP(r *http.Request) string {
	if h.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.svc.TTL().Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
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

	token, u, err := h.svc.Login(r.Context(), req.Email, req.Password, h.clientIP(r))
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return httpx.Unauthorized("メールアドレスまたはパスワードが正しくありません")
	case errors.Is(err, ErrThrottled):
		return &httpx.Error{Status: http.StatusTooManyRequests, Code: "too_many_attempts", Message: "ログインの失敗が続いたため、しばらくログインできません。15分ほど待ってからやり直してください"}
	case errors.Is(err, ErrPasswordNotAllowed):
		return &httpx.Error{Status: http.StatusForbidden, Code: "sso_required", Message: "Google アカウントでログインしてください（パスワードでのログインは FP&A の非常用だけです）"}
	case err != nil:
		return err
	}
	h.setSession(w, token)
	httpx.WriteJSON(w, http.StatusOK, map[string]User{"user": u})
	return nil
}

// Config は GET /api/auth/config。ログイン画面用に、SSO が有効か、パスワードでログインできるロールを返す（未ログインで呼べる）。
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) error {
	roles := h.svc.PasswordRoles()
	if roles == nil {
		roles = []Role{RoleFPAAdmin, RoleManager, RoleMember, RoleViewer}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sso": h.oidc != nil, "password_roles": roles})
	return nil
}

const oidcStateCookie = "fpanda_oidc"

// OIDCLogin は GET /api/auth/oidc/login?return_to=/path。検証値を短命の Cookie に入れ、IdP へリダイレクトする。
func (h *Handler) OIDCLogin(w http.ResponseWriter, r *http.Request) error {
	if h.oidc == nil {
		return httpx.NotFound("SSO は設定されていません")
	}
	st, err := newAuthState(r.URL.Query().Get("return_to"))
	if err != nil {
		return err
	}
	target, err := h.oidc.AuthURL(r.Context(), st)
	if err != nil {
		h.logger.Error("oidc: discovery", "error", err)
		http.Redirect(w, r, "/?login_error=failed", http.StatusFound)
		return nil
	}
	b, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    base64.RawURLEncoding.EncodeToString(b),
		Path:     "/api/auth/oidc",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode, // IdP からのリダイレクト（GET）で送られる
	})
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// OIDCCallback は GET /api/auth/oidc/callback。ID トークンを検証してセッションを作り、元の画面へ戻す。
// 失敗したら /?login_error=<理由> へ戻す（not_registered / domain / failed）。
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) error {
	if h.oidc == nil {
		return httpx.NotFound("SSO は設定されていません")
	}
	fail := func(reason string, err error) error {
		h.logger.Warn("oidc: login failed", "reason", reason, "error", err)
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(reason), http.StatusFound)
		return nil
	}
	// 検証値の Cookie は一度だけ使う
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode})
	c, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return fail("failed", errors.New("state cookie missing"))
	}
	var st authState
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(b, &st) != nil || st.State == "" {
		return fail("failed", errors.New("invalid state cookie"))
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		return fail("failed", errors.New("idp: "+q.Get("error")))
	}
	if q.Get("state") != st.State {
		return fail("failed", errors.New("state mismatch"))
	}
	claims, err := h.oidc.Exchange(r.Context(), q.Get("code"), st)
	switch {
	case errors.Is(err, ErrDomainNotAllowed):
		return fail("domain", err)
	case errors.Is(err, ErrNotRegistered):
		return fail("not_registered", err)
	case err != nil:
		return fail("failed", err)
	}
	token, _, err := h.svc.LoginOIDC(r.Context(), claims.Email, claims.Sub)
	switch {
	case errors.Is(err, ErrNotRegistered), errors.Is(err, ErrSubjectMismatch):
		return fail("not_registered", err)
	case err != nil:
		return err
	}
	h.setSession(w, token)
	http.Redirect(w, r, st.ReturnTo, http.StatusFound)
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
