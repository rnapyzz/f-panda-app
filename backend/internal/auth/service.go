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
	user, err := s.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return db.AppUser{}, ErrInvalidCredentials
		}
		return db.AppUser{}, err
	}

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

	user, err := s.Queries.GetUserByID(ctx, session.UserID)
	if err != nil {
		return db.AppUser{}, errNoSession
	}

	_ = s.Queries.TouchSession(ctx, session.TokenHash)

	return user, nil
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
