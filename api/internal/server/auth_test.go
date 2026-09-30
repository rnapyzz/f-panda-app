package server_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/password"
	"github.com/rnapyzz/f-panda-app/api/internal/server"
	"github.com/rnapyzz/f-panda-app/api/internal/testutil"
)

const testPassword = "test-password-123"

// testServer は実際の DB を使った API サーバーを起動する。
func testServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.NewDB(t)
	h := server.NewHandler(server.Deps{
		DB:     db,
		Auth:   auth.NewService(db, time.Hour),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, db
}

func createUser(t *testing.T, db *sql.DB, email string, role auth.Role) int64 {
	t.Helper()
	hash, err := password.Hash(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec("INSERT INTO users (name, email, password_hash, role) VALUES (?, ?, ?, ?)", "テスト "+string(role), email, hash, role)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// client は Cookie を保持する HTTP クライアント。
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(res.Body)
	if len(b) > 0 {
		if err := json.Unmarshal(b, &out); err != nil {
			c.t.Fatalf("%s %s: invalid JSON response %q", method, path, b)
		}
	}
	return res.StatusCode, out
}

func (c *client) login(email string) {
	c.t.Helper()
	status, body := c.do("POST", "/api/auth/login", map[string]string{"email": email, "password": testPassword})
	if status != http.StatusOK {
		c.t.Fatalf("login %s: status = %d, body = %v", email, status, body)
	}
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestLoginAndMe(t *testing.T) {
	srv, db := testServer(t)
	createUser(t, db, "admin@example.com", auth.RoleFPAAdmin)
	c := newClient(t, srv)

	if status, _ := c.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Fatalf("未ログインの /me: status = %d, want 401", status)
	}

	// メールアドレスは大文字・前後空白を正規化して照合する
	status, body := c.do("POST", "/api/auth/login", map[string]string{"email": "  Admin@Example.com ", "password": testPassword})
	if status != http.StatusOK {
		t.Fatalf("login: status = %d, body = %v", status, body)
	}

	status, body = c.do("GET", "/api/auth/me", nil)
	if status != http.StatusOK {
		t.Fatalf("/me: status = %d", status)
	}
	u := body["user"].(map[string]any)
	if u["email"] != "admin@example.com" || u["role"] != "fpa_admin" {
		t.Errorf("/me user = %v", u)
	}
	if _, ok := u["password_hash"]; ok {
		t.Error("レスポンスに password_hash が含まれている")
	}
}

func TestLoginFailures(t *testing.T) {
	srv, db := testServer(t)
	id := createUser(t, db, "member@example.com", auth.RoleMember)
	c := newClient(t, srv)

	tests := []struct {
		name  string
		body  map[string]string
		want  int
		setup func()
	}{
		{name: "パスワード誤り", body: map[string]string{"email": "member@example.com", "password": "wrong-password-1"}, want: http.StatusUnauthorized},
		{name: "未登録のメールアドレス", body: map[string]string{"email": "nobody@example.com", "password": testPassword}, want: http.StatusUnauthorized},
		{name: "未入力", body: map[string]string{"email": "", "password": ""}, want: http.StatusUnprocessableEntity},
		{
			name:  "無効化されたユーザー",
			body:  map[string]string{"email": "member@example.com", "password": testPassword},
			want:  http.StatusUnauthorized,
			setup: func() { db.Exec("UPDATE users SET is_active = FALSE WHERE id = ?", id) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			if status, body := c.do("POST", "/api/auth/login", tt.body); status != tt.want {
				t.Errorf("status = %d, want %d, body = %v", status, tt.want, body)
			}
		})
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	srv, db := testServer(t)
	createUser(t, db, "viewer@example.com", auth.RoleViewer)
	c := newClient(t, srv)
	c.login("viewer@example.com")

	// ログアウト前の Cookie を保存しておき、ログアウト後に再送しても使えないことを確認する
	cookies := c.http.Jar.Cookies(mustURL(t, srv.URL))

	if status, _ := c.do("POST", "/api/auth/logout", nil); status != http.StatusNoContent {
		t.Fatalf("logout: status = %d", status)
	}
	if status, _ := c.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("ログアウト後の /me: status = %d, want 401", status)
	}

	c.http.Jar.SetCookies(mustURL(t, srv.URL), cookies)
	if status, _ := c.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("古い Cookie での /me: status = %d, want 401", status)
	}
}

func TestDeactivatedUserLosesSession(t *testing.T) {
	srv, db := testServer(t)
	id := createUser(t, db, "manager@example.com", auth.RoleManager)
	c := newClient(t, srv)
	c.login("manager@example.com")

	db.Exec("UPDATE users SET is_active = FALSE WHERE id = ?", id)
	if status, _ := c.do("GET", "/api/auth/me", nil); status != http.StatusUnauthorized {
		t.Errorf("無効化後の /me: status = %d, want 401", status)
	}
}

func TestCrossOriginRequestRejected(t *testing.T) {
	srv, db := testServer(t)
	createUser(t, db, "admin@example.com", auth.RoleFPAAdmin)

	req, _ := http.NewRequest("POST", srv.URL+"/api/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"`+testPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("別オリジンからの POST: status = %d, want 403", res.StatusCode)
	}
}

func TestUnknownAPIPathReturnsJSON404(t *testing.T) {
	srv, _ := testServer(t)
	status, body := newClient(t, srv).do("GET", "/api/nope", nil)
	if status != http.StatusNotFound || errorCode(body) != "not_found" {
		t.Errorf("status = %d, body = %v", status, body)
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
