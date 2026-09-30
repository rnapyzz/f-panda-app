// Package auth はメールアドレス＋パスワードによるログインとセッション管理を行う。
//
// セッショントークンはランダムな32バイトを Cookie で渡し、DB には SHA-256 ハッシュのみ保存する。
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/rnapyzz/f-panda-app/api/internal/password"
)

// Role はユーザーのロール。
type Role string

const (
	RoleFPAAdmin Role = "fpa_admin"
	RoleManager  Role = "manager"
	RoleMember   Role = "member"
	RoleViewer   Role = "viewer"
)

// Valid は定義済みのロールかを返す。
func (r Role) Valid() bool {
	switch r {
	case RoleFPAAdmin, RoleManager, RoleMember, RoleViewer:
		return true
	}
	return false
}

// User はログイン中のユーザー。
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

var (
	// ErrInvalidCredentials はメールアドレスまたはパスワードが誤っているとき、
	// またはユーザーが無効化されているときに返る。どれに該当するかは区別しない。
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrNoSession はセッションが存在しない・期限切れ・ユーザーが無効化されているときに返る。
	ErrNoSession = errors.New("auth: no valid session")
)

// ユーザーが存在しない場合もハッシュ計算を行い、応答時間からメールアドレスの登録有無を推測されないようにする。
var dummyHash, _ = password.Hash("dummy password for timing")

// Service はログインとセッションを扱う。
type Service struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

// NewService は Service を作る。ttl はセッションの有効期間。
func NewService(db *sql.DB, ttl time.Duration) *Service {
	return &Service{db: db, ttl: ttl, now: time.Now}
}

// TTL はセッションの有効期間を返す。
func (s *Service) TTL() time.Duration { return s.ttl }

// Login はメールアドレスとパスワードを検証し、新しいセッションを作る。
func (s *Service) Login(ctx context.Context, email, pw string) (token string, u User, err error) {
	var hash string
	var active bool
	err = s.db.QueryRowContext(ctx,
		"SELECT id, name, email, role, password_hash, is_active FROM users WHERE email = ?",
		NormalizeEmail(email),
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &hash, &active)
	if errors.Is(err, sql.ErrNoRows) {
		password.Verify(pw, dummyHash)
		return "", User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", User{}, err
	}

	ok, err := password.Verify(pw, hash)
	if errors.Is(err, password.ErrInvalidHash) {
		// パスワード未設定（CSV で追加したユーザーなど）はログインできない
		return "", User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", User{}, err
	}
	if !ok || !active {
		return "", User{}, ErrInvalidCredentials
	}

	token, err = newToken()
	if err != nil {
		return "", User{}, err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		hashToken(token), u.ID, s.now().Add(s.ttl),
	)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// Authenticate はセッショントークンからユーザーを取得する。
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrNoSession
	}
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.email, u.role
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > ? AND u.is_active`,
		hashToken(token), s.now(),
	).Scan(&u.ID, &u.Name, &u.Email, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoSession
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// Logout はセッションを削除する。
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", hashToken(token))
	return err
}

// DeleteExpiredSessions は期限切れのセッションを削除する。
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", s.now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// NormalizeEmail はメールアドレスを比較用に正規化する（前後の空白除去・小文字化）。
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
