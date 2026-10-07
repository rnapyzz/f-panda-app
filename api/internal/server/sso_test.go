package server_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/server"
	"github.com/rnapyzz/f-panda-app/api/internal/testutil"
)

// fakeIdP は OIDC の IdP の代わり（discovery・JWKS・トークンエンドポイント）。
// 次に発行する ID トークンの内容を claims で決める。
type fakeIdP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	claims map[string]any
	// verifier はトークンの交換で受け取った PKCE の検証値
	verifier string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.srv.URL,
			"authorization_endpoint": idp.srv.URL + "/authorize",
			"token_endpoint":         idp.srv.URL + "/token",
			"jwks_uri":               idp.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		idp.mu.Lock()
		idp.verifier = r.Form.Get("code_verifier")
		claims := idp.claims
		idp.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"id_token": idp.sign(claims), "token_type": "Bearer"})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIdP) sign(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ssoServer は SSO を有効にした API サーバー（パスワードでのログインは FP&A だけ）。
func ssoServer(t *testing.T, idp *fakeIdP) (*httptest.Server, *testEnv) {
	db := testutil.NewDB(t)
	svc := auth.NewService(db, time.Hour)
	svc.RestrictPasswordLogin(auth.RoleFPAAdmin)
	h := server.NewHandler(server.Deps{
		DB:     db,
		Auth:   svc,
		OIDC:   auth.NewOIDC(auth.OIDCConfig{Issuer: idp.srv.URL, ClientID: "client-1", ClientSecret: "secret", RedirectURL: "http://app/api/auth/oidc/callback", AllowedDomains: []string{"example.com"}}),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, &testEnv{DB: db, server: srv}
}

// ssoLogin は SSO のログインを最後まで進め、コールバックのリダイレクト先を返す。claims の nonce はログインの開始で決まる。
func ssoLogin(t *testing.T, srv *httptest.Server, idp *fakeIdP, claims map[string]any) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Get(srv.URL + "/api/auth/oidc/login?return_to=/reports")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	q := loc.Query()
	if res.StatusCode != http.StatusFound || !strings.HasPrefix(loc.String(), idp.srv.URL+"/authorize") || q.Get("code_challenge_method") != "S256" || q.Get("hd") != "example.com" {
		t.Fatalf("ログインの開始: status = %d, location = %s", res.StatusCode, loc)
	}
	full := map[string]any{"iss": idp.srv.URL, "aud": "client-1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": q.Get("nonce"), "email_verified": true}
	for k, v := range claims {
		full[k] = v
	}
	idp.mu.Lock()
	idp.claims = full
	idp.mu.Unlock()
	res, err = c.Get(srv.URL + "/api/auth/oidc/callback?code=abc&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	// PKCE: トークンの交換で送った検証値が、ログインの開始で送った challenge と一致する
	idp.mu.Lock()
	sum := sha256.Sum256([]byte(idp.verifier))
	idp.mu.Unlock()
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
		t.Errorf("PKCE の検証値が一致しない")
	}
	return c, res.Header.Get("Location")
}

func TestSSOLogin(t *testing.T) {
	idp := newFakeIdP(t)
	srv, env := ssoServer(t, idp)
	createUser(t, env.DB, "member@example.com", auth.RoleMember)
	createUser(t, env.DB, "admin@example.com", auth.RoleFPAAdmin)

	// 登録済みのユーザーはログインでき、元の画面へ戻る。初回は sub を記録する
	c, loc := ssoLogin(t, srv, idp, map[string]any{"sub": "g-1", "email": "Member@example.com", "hd": "example.com"})
	if loc != "/reports" {
		t.Fatalf("リダイレクト先 = %q", loc)
	}
	res, _ := c.Get(srv.URL + "/api/auth/me")
	if res.StatusCode != http.StatusOK {
		t.Errorf("SSO でログインした後の /me: status = %d", res.StatusCode)
	}
	var sub string
	env.QueryRow("SELECT oidc_subject FROM users WHERE email = 'member@example.com'").Scan(&sub)
	if sub != "g-1" {
		t.Errorf("記録した sub = %q", sub)
	}

	cases := []struct {
		name   string
		claims map[string]any
		want   string
	}{
		{"未登録", map[string]any{"sub": "g-9", "email": "nobody@example.com", "hd": "example.com"}, "/?login_error=not_registered"},
		{"別の sub（メールアドレスの付け替え）", map[string]any{"sub": "g-2", "email": "member@example.com", "hd": "example.com"}, "/?login_error=not_registered"},
		{"許可していないドメイン", map[string]any{"sub": "g-3", "email": "admin@other.com", "hd": "other.com"}, "/?login_error=domain"},
		{"メールアドレスが未確認", map[string]any{"sub": "g-4", "email": "admin@example.com", "email_verified": false}, "/?login_error=not_registered"},
		{"期限切れ", map[string]any{"sub": "g-5", "email": "admin@example.com", "exp": time.Now().Add(-time.Hour).Unix()}, "/?login_error=failed"},
		{"宛先の違い", map[string]any{"sub": "g-6", "email": "admin@example.com", "aud": "other-client"}, "/?login_error=failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, loc := ssoLogin(t, srv, idp, tc.claims); loc != tc.want {
				t.Errorf("リダイレクト先 = %q, want %q", loc, tc.want)
			}
		})
	}

	// state が違えば通さない
	jar, _ := cookiejar.New(nil)
	nc := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, _ := nc.Get(srv.URL + "/api/auth/oidc/login")
	r.Body.Close()
	r, _ = nc.Get(srv.URL + "/api/auth/oidc/callback?code=abc&state=wrong")
	r.Body.Close()
	if loc := r.Header.Get("Location"); loc != "/?login_error=failed" {
		t.Errorf("state の違い: location = %q", loc)
	}

	// SSO が有効な環境では、パスワードでログインできるのは FP&A だけ
	member := newClient(t, srv)
	if status, body := member.do("POST", "/api/auth/login", map[string]string{"email": "member@example.com", "password": testPassword}); status != http.StatusForbidden || errorCode(body) != "sso_required" {
		t.Errorf("担当者のパスワードでのログイン: status = %d, body = %v", status, body)
	}
	newClient(t, srv).login("admin@example.com")
	status, body := member.do("GET", "/api/auth/config", nil)
	if status != http.StatusOK || body["sso"] != true || len(body["password_roles"].([]any)) != 1 {
		t.Errorf("/api/auth/config = %d %v", status, body)
	}
}

func TestLoginThrottle(t *testing.T) {
	srv, db := testServer(t)
	createUser(t, db, "member@example.com", auth.RoleMember)
	c := newClient(t, srv)
	for i := 0; i < 5; i++ {
		if status, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "member@example.com", "password": "wrong-password-123"}); status != http.StatusUnauthorized {
			t.Fatalf("%d 回目の失敗: status = %d", i+1, status)
		}
	}
	// 5回失敗すると、正しいパスワードでも止める
	if status, body := c.do("POST", "/api/auth/login", map[string]string{"email": "member@example.com", "password": testPassword}); status != http.StatusTooManyRequests || errorCode(body) != "too_many_attempts" {
		t.Errorf("制限中のログイン: status = %d, body = %v", status, body)
	}
	// SSO が無効な環境では、だれでもパスワードでログインできる
	if body := newClient(t, srv).mustGet("/api/auth/config"); body["sso"] != false || len(body["password_roles"].([]any)) != 4 {
		t.Errorf("/api/auth/config = %v", body)
	}
}
