// Package auth implements server-side session authentication (HttpOnly
// session cookie + non-HttpOnly CSRF cookie, double-submit pattern) rather
// than JWTs in local storage, to keep bearer credentials out of reach of XSS.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "fpanda_session"
	csrfCookieName    = "fpanda_csrf"
	sessionTTL        = 12 * time.Hour
)

var ErrInvalidCredentials = errors.New("auth: invalid email or password")

type Service struct {
	Queries      *db.Queries
	CookieSecure bool
}

func NewService(q *db.Queries, cookieSecure bool) *Service {
	return &Service{Queries: q, CookieSecure: cookieSecure}
}

// Login verifies credentials, opens a session, and sets the session + CSRF
// cookies on the response.
func (s *Service) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, email, password string) (db.AppUser, error) {
	row, err := s.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.AppUser{}, ErrInvalidCredentials
		}
		return db.AppUser{}, err
	}
	user := appUserFromGetByEmailRow(row)

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return db.AppUser{}, ErrInvalidCredentials
	}

	sessionToken, err := newOpaqueToken()
	if err != nil {
		return db.AppUser{}, err
	}
	csrfToken, err := newOpaqueToken()
	if err != nil {
		return db.AppUser{}, err
	}

	expiresAt := time.Now().Add(sessionTTL)
	err = s.Queries.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hashToken(sessionToken),
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		IpAddress: sql.NullString{String: clientIP(r), Valid: true},
		UserAgent: sql.NullString{String: r.UserAgent(), Valid: r.UserAgent() != ""},
	})
	if err != nil {
		return db.AppUser{}, err
	}

	s.setCookie(w, sessionCookieName, sessionToken, expiresAt, true)
	s.setCookie(w, csrfCookieName, csrfToken, expiresAt, false)

	return user, nil
}

// Logout deletes the current session (if any) and clears both cookies.
func (s *Service) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if err := s.Queries.DeleteSession(ctx, hashToken(c.Value)); err != nil {
			return err
		}
	}
	s.clearCookie(w, sessionCookieName, true)
	s.clearCookie(w, csrfCookieName, false)
	return nil
}

// CurrentUser resolves the session cookie on the request to the owning user.
// Returns sql.ErrNoRows-wrapping errors when there is no valid session.
func (s *Service) CurrentUser(ctx context.Context, r *http.Request) (db.AppUser, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return db.AppUser{}, errNoSession
	}

	session, err := s.Queries.GetSessionByTokenHash(ctx, hashToken(c.Value))
	if err != nil {
		return db.AppUser{}, errNoSession
	}

	row, err := s.Queries.GetUserByID(ctx, session.UserID)
	if err != nil {
		return db.AppUser{}, errNoSession
	}

	_ = s.Queries.TouchSession(ctx, session.TokenHash)

	return appUserFromGetByIDRow(row), nil
}

// GetUserByEmail/GetUserByID now select department_id in addition to
// app_user's other columns, so sqlc generates row-specific structs instead
// of aliasing to the AppUser model — convert back since db.AppUser is the
// currency type for "the authenticated user" across every other package.
func appUserFromGetByEmailRow(r db.GetUserByEmailRow) db.AppUser {
	return db.AppUser{
		ID: r.ID, Email: r.Email, Name: r.Name, Role: r.Role, DepartmentID: r.DepartmentID,
		PasswordHash: r.PasswordHash, IsActive: r.IsActive, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func appUserFromGetByIDRow(r db.GetUserByIDRow) db.AppUser {
	return db.AppUser{
		ID: r.ID, Email: r.Email, Name: r.Name, Role: r.Role, DepartmentID: r.DepartmentID,
		PasswordHash: r.PasswordHash, IsActive: r.IsActive, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

var errNoSession = errors.New("auth: no valid session")

func (s *Service) setCookie(w http.ResponseWriter, name, value string, expiresAt time.Time, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: httpOnly,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearCookie(w http.ResponseWriter, name string, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: httpOnly,
		Secure:   s.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ListUsers is used by the Phase 4 user-assignment admin UI to populate the
// "assign this field user" picker.
func (s *Service) ListUsers(ctx context.Context) ([]db.ListUsersRow, error) {
	return s.Queries.ListUsers(ctx)
}

func departmentIDParam(departmentID *uint64) sql.NullInt64 {
	if departmentID == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*departmentID), Valid: true}
}

// CreateUser creates a login account directly with an admin-chosen initial
// password — there's no email/invite infrastructure yet, so this mirrors how
// cmd/seed already bootstraps the first office_admin account.
func (s *Service) CreateUser(ctx context.Context, email, name string, role db.AppUserRole, departmentID *uint64, password string) (int64, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return 0, err
	}
	return s.Queries.CreateUser(ctx, db.CreateUserParams{
		Email: email, Name: name, Role: role, DepartmentID: departmentIDParam(departmentID), PasswordHash: hash,
	})
}

func (s *Service) UpdateUser(ctx context.Context, id uint64, name string, role db.AppUserRole, departmentID *uint64, isActive bool) error {
	return s.Queries.UpdateUser(ctx, db.UpdateUserParams{
		ID: id, Name: name, Role: role, DepartmentID: departmentIDParam(departmentID), IsActive: isActive,
	})
}

func (s *Service) ResetPassword(ctx context.Context, id uint64, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.Queries.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{ID: id, PasswordHash: hash})
}

// HashPassword is exposed for the user-seeding tool.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
