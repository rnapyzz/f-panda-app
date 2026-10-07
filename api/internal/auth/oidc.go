package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
)

// SSO（OIDC）のログイン（docs/architecture.md「SSO（OIDC）」）。
// 認可コードフロー（PKCE・state・nonce 付き）で ID トークンを受け取り、標準ライブラリだけで署名と内容を検証する。
// 事前に登録したユーザーだけがログインできる（メールアドレスで照合し、初回に sub を記録して以後も照合する）。

// OIDCConfig は OIDC の設定。ClientID が空なら SSO は無効。
type OIDCConfig struct {
	Issuer         string // 例: https://accounts.google.com
	ClientID       string
	ClientSecret   string
	RedirectURL    string   // 例: https://fpanda.example.com/api/auth/oidc/callback
	AllowedDomains []string // 例: example.com。空ならドメインを制限しない
}

// Enabled は SSO が有効かを返す。
func (c OIDCConfig) Enabled() bool { return c.ClientID != "" }

// OIDC の検証の失敗。ログイン画面に返す理由（error クエリ）に対応する。
var (
	// ErrNotRegistered はアプリに登録されていない（または無効な）ユーザー
	ErrNotRegistered = errors.New("auth: user not registered")
	// ErrDomainNotAllowed は許可していないドメインのアカウント
	ErrDomainNotAllowed = errors.New("auth: domain not allowed")
	// ErrSubjectMismatch は、記録した SSO のアカウント ID と一致しない
	ErrSubjectMismatch = errors.New("auth: subject mismatch")
)

const (
	discoveryTTL = time.Hour
	jwksTTL      = time.Hour
	clockSkew    = 2 * time.Minute
)

// OIDC は IdP の設定（discovery）と公開鍵（JWKS）をキャッシュし、ID トークンを検証する。
type OIDC struct {
	cfg    OIDCConfig
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	discovery *discovery
	discAt    time.Time
	keys      map[string]*rsa.PublicKey
	keysAt    time.Time
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// NewOIDC は OIDC を作る。IdP の設定は最初に使うときに読む（起動時に IdP へ接続できなくてもよい）。
func NewOIDC(cfg OIDCConfig) *OIDC {
	if cfg.Issuer == "" {
		cfg.Issuer = "https://accounts.google.com"
	}
	return &OIDC{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, now: time.Now}
}

func (o *OIDC) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	res, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: GET %s: %d", u, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(v)
}

func (o *OIDC) loadDiscovery(ctx context.Context) (*discovery, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.discovery != nil && o.now().Sub(o.discAt) < discoveryTTL {
		return o.discovery, nil
	}
	var d discovery
	if err := o.getJSON(ctx, strings.TrimRight(o.cfg.Issuer, "/")+"/.well-known/openid-configuration", &d); err != nil {
		return nil, err
	}
	if d.Issuer != o.cfg.Issuer || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: invalid discovery document (issuer %q)", d.Issuer)
	}
	o.discovery, o.discAt = &d, o.now()
	return o.discovery, nil
}

// publicKey は kid の公開鍵を返す。見つからなければ JWKS を読み直す（鍵の更新に追随する）。
func (o *OIDC) publicKey(ctx context.Context, jwksURI, kid string) (*rsa.PublicKey, error) {
	o.mu.Lock()
	key, ok := o.keys[kid]
	fresh := o.now().Sub(o.keysAt) < jwksTTL
	o.mu.Unlock()
	if ok && fresh {
		return key, nil
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := o.getJSON(ctx, jwksURI, &set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	o.mu.Lock()
	o.keys, o.keysAt = keys, o.now()
	o.mu.Unlock()
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("oidc: unknown key id %q", kid)
}

// authState はログインの開始から戻りまでの検証値（短命の Cookie に入れる）。
type authState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// newAuthState は state・nonce・PKCE の検証値を作る。returnTo はログイン後に戻るパス（アプリ内の相対パスだけ）。
func newAuthState(returnTo string) (authState, error) {
	var st authState
	var err error
	if st.State, err = randomString(); err != nil {
		return st, err
	}
	if st.Nonce, err = randomString(); err != nil {
		return st, err
	}
	if st.Verifier, err = randomString(); err != nil {
		return st, err
	}
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") || strings.HasPrefix(returnTo, "/api/") {
		returnTo = "/"
	}
	st.ReturnTo = returnTo
	return st, nil
}

// AuthURL は IdP の認可エンドポイントの URL を返す。
func (o *OIDC) AuthURL(ctx context.Context, st authState) (string, error) {
	d, err := o.loadDiscovery(ctx)
	if err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(st.Verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {o.cfg.ClientID},
		"redirect_uri":          {o.cfg.RedirectURL},
		"scope":                 {"openid email profile"},
		"state":                 {st.State},
		"nonce":                 {st.Nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	if len(o.cfg.AllowedDomains) == 1 {
		q.Set("hd", o.cfg.AllowedDomains[0]) // Google: アカウントの選択をそのドメインに絞る（検証はトークンで行う）
	}
	return d.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// idClaims は ID トークンの内容のうち、使うもの。
type idClaims struct {
	Iss           string          `json:"iss"`
	Sub           string          `json:"sub"`
	Aud           json.RawMessage `json:"aud"` // 文字列か配列
	Exp           int64           `json:"exp"`
	Iat           int64           `json:"iat"`
	Nonce         string          `json:"nonce"`
	Email         string          `json:"email"`
	EmailVerified any             `json:"email_verified"` // true か "true"
	Hd            string          `json:"hd"`
}

// Exchange は認可コードを ID トークンに交換し、検証した内容を返す。
func (o *OIDC) Exchange(ctx context.Context, code string, st authState) (idClaims, error) {
	d, err := o.loadDiscovery(ctx)
	if err != nil {
		return idClaims{}, err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {o.cfg.RedirectURL},
		"client_id":     {o.cfg.ClientID},
		"client_secret": {o.cfg.ClientSecret},
		"code_verifier": {st.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return idClaims{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := o.client.Do(req)
	if err != nil {
		return idClaims{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return idClaims{}, fmt.Errorf("oidc: token endpoint: %d %s", res.StatusCode, b)
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tok); err != nil {
		return idClaims{}, err
	}
	return o.verify(ctx, d, tok.IDToken, st.Nonce)
}

// verify は ID トークン（RS256 の JWT）の署名と内容を検証する。
func (o *OIDC) verify(ctx context.Context, d *discovery, token, nonce string) (idClaims, error) {
	var c idClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, errors.New("oidc: malformed id token")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return c, err
	}
	if header.Alg != "RS256" {
		return c, fmt.Errorf("oidc: unsupported alg %q", header.Alg)
	}
	key, err := o.publicKey(ctx, d.JWKSURI, header.Kid)
	if err != nil {
		return c, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return c, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return c, errors.New("oidc: invalid signature")
	}
	if err := decodeSegment(parts[1], &c); err != nil {
		return c, err
	}
	now := o.now()
	switch {
	case c.Iss != d.Issuer:
		return c, fmt.Errorf("oidc: unexpected issuer %q", c.Iss)
	case !audienceContains(c.Aud, o.cfg.ClientID):
		return c, errors.New("oidc: unexpected audience")
	case now.After(time.Unix(c.Exp, 0).Add(clockSkew)):
		return c, errors.New("oidc: id token expired")
	case time.Unix(c.Iat, 0).After(now.Add(clockSkew)):
		return c, errors.New("oidc: id token issued in the future")
	case c.Nonce != nonce:
		return c, errors.New("oidc: nonce mismatch")
	case c.Sub == "" || c.Email == "":
		return c, errors.New("oidc: missing sub or email")
	case c.EmailVerified != true && c.EmailVerified != "true":
		return c, ErrNotRegistered
	}
	if len(o.cfg.AllowedDomains) > 0 {
		domain := c.Hd
		if domain == "" {
			domain = c.Email[strings.LastIndex(c.Email, "@")+1:]
		}
		if !slices.Contains(o.cfg.AllowedDomains, strings.ToLower(domain)) {
			return c, ErrDomainNotAllowed
		}
	}
	return c, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return fmt.Errorf("oidc: malformed segment: %w", err)
	}
	return json.Unmarshal(b, v)
}

func audienceContains(raw json.RawMessage, clientID string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == clientID
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return slices.Contains(many, clientID)
	}
	return false
}

// LoginOIDC は SSO で確かめたアカウントのユーザーでセッションを作る。
// 未登録・無効なら ErrNotRegistered、記録した sub と一致しなければ ErrSubjectMismatch。初回は sub を記録する。
func (s *Service) LoginOIDC(ctx context.Context, email, sub string) (string, User, error) {
	var u User
	var active bool
	var subject sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT id, name, email, role, is_active, oidc_subject FROM users WHERE email = ?", NormalizeEmail(email)).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &active, &subject)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !active) {
		return "", User{}, ErrNotRegistered
	}
	if err != nil {
		return "", User{}, err
	}
	switch {
	case subject.Valid && subject.String != sub:
		return "", User{}, ErrSubjectMismatch
	case !subject.Valid:
		_, err := s.db.ExecContext(ctx, "UPDATE users SET oidc_subject = ? WHERE id = ? AND oidc_subject IS NULL", sub, u.ID)
		if dbx.ErrNo(err) == dbx.ErrDuplicateEntry {
			return "", User{}, ErrSubjectMismatch // 別のユーザーに記録済みのアカウント
		}
		if err != nil {
			return "", User{}, err
		}
	}
	token, err := s.createSession(ctx, u.ID)
	return token, u, err
}
