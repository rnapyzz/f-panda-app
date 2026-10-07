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
	"slices"
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
	// ErrPasswordNotAllowed は、SSO が有効な環境で、パスワードでのログインが許されていないロールのときに返る。
	ErrPasswordNotAllowed = errors.New("auth: password login not allowed")
)

// ユーザーが存在しない場合もハッシュ計算を行い、応答時間からメールアドレスの登録有無を推測されないようにする。
var dummyHash, _ = password.Hash("dummy password for timing")

// Service はログインとセッションを扱う。
type Service struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
	// passwordRoles はパスワードでログインできるロール。nil ならすべて（SSO が無効な環境）
	passwordRoles []Role
}

// NewService は Service を作る。ttl はセッションの有効期間。
func NewService(db *sql.DB, ttl time.Duration) *Service {
	return &Service{db: db, ttl: ttl, now: time.Now}
}

// RestrictPasswordLogin は、パスワードでログインできるロールを制限する（SSO が有効な環境で FP&A の非常用だけにする）。
func (s *Service) RestrictPasswordLogin(roles ...Role) { s.passwordRoles = roles }

// PasswordRoles はパスワードでログインできるロール（nil はすべて）。
func (s *Service) PasswordRoles() []Role { return s.passwordRoles }

// TTL はセッションの有効期間を返す。
func (s *Service) TTL() time.Duration { return s.ttl }

// Login はメールアドレスとパスワードを検証し、新しいセッションを作る。ip はログイン失敗の制限に使う。
// 失敗が多すぎれば ErrThrottled、パスワードでのログインが許されていないロールなら ErrPasswordNotAllowed を返す。
func (s *Service) Login(ctx context.Context, email, pw, ip string) (token string, u User, err error) {
	email = NormalizeEmail(email)
	if limited, err := s.throttled(ctx, email, ip); err != nil {
		return "", User{}, err
	} else if limited {
		return "", User{}, ErrThrottled
	}
	token, u, err = s.login(ctx, email, pw)
	if errors.Is(err, ErrInvalidCredentials) {
		if rerr := s.recordFailure(ctx, email, ip); rerr != nil {
			return "", User{}, rerr
		}
		return "", User{}, err
	}
	if err != nil {
		return "", User{}, err
	}
	return token, u, s.clearFailures(ctx, email)
}

func (s *Service) login(ctx context.Context, email, pw string) (token string, u User, err error) {
	var hash string
	var active bool
	err = s.db.QueryRowContext(ctx,
		"SELECT id, name, email, role, password_hash, is_active FROM users WHERE email = ?",
		email,
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
	if s.passwordRoles != nil && !slices.Contains(s.passwordRoles, u.Role) {
		return "", User{}, ErrPasswordNotAllowed
	}
	token, err = s.createSession(ctx, u.ID)
	return token, u, err
}

// createSession は新しいセッションを作り、トークンを返す。
func (s *Service) createSession(ctx context.Context, userID int64) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		hashToken(token), userID, s.now().Add(s.ttl),
	)
	return token, err
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

// DeleteExpiredSessions は期限切れのセッションと、古いログインの失敗の記録を削除する。
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	if err := s.deleteOldAttempts(ctx); err != nil {
		return 0, err
	}
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
