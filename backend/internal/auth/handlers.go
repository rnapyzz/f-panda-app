package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID    uint64 `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

func toUserResponse(u db.AppUser) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)}
}

func (s *Service) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := s.Login(r.Context(), w, r, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}
		slog.Error("login failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := audit.Record(r.Context(), s.Queries, audit.Params{
		UserID: &user.ID, Action: audit.ActionLogin, EntityType: audit.EntityUser, EntityID: &user.ID,
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", audit.ActionLogin)
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *Service) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	// Resolve the user before Logout() deletes the session it depends on.
	user, hasUser := UserFromContext(r.Context())

	if err := s.Logout(r.Context(), w, r); err != nil {
		slog.Error("logout failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if hasUser {
		if err := audit.Record(r.Context(), s.Queries, audit.Params{
			UserID: &user.ID, Action: audit.ActionLogout, EntityType: audit.EntityUser, EntityID: &user.ID,
			IPAddress: httpx.ClientIP(r),
		}); err != nil {
			slog.Error("audit log write failed", "error", err, "action", audit.ActionLogout)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) MeHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

type listUserDTO struct {
	ID       uint64 `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
}

func (s *Service) ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListUsers(r.Context())
	if err != nil {
		slog.Error("list users failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]listUserDTO, len(rows))
	for i, row := range rows {
		out[i] = listUserDTO{ID: row.ID, Email: row.Email, Name: row.Name, Role: string(row.Role), IsActive: row.IsActive}
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
