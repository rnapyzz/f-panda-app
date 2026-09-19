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
	ID           uint64  `json:"id"`
	Email        string  `json:"email"`
	Name         string  `json:"name"`
	Role         string  `json:"role"`
	DepartmentID *uint64 `json:"department_id,omitempty"`
}

func toUserResponse(u db.AppUser) userResponse {
	resp := userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)}
	if u.DepartmentID.Valid {
		id := uint64(u.DepartmentID.Int64)
		resp.DepartmentID = &id
	}
	return resp
}

var validRoles = map[string]db.AppUserRole{
	"field_user":   db.AppUserRoleFieldUser,
	"office_admin": db.AppUserRoleOfficeAdmin,
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
	ID           uint64  `json:"id"`
	Email        string  `json:"email"`
	Name         string  `json:"name"`
	Role         string  `json:"role"`
	DepartmentID *uint64 `json:"department_id,omitempty"`
	IsActive     bool    `json:"is_active"`
}

func toListUserDTO(row db.ListUsersRow) listUserDTO {
	dto := listUserDTO{ID: row.ID, Email: row.Email, Name: row.Name, Role: string(row.Role), IsActive: row.IsActive}
	if row.DepartmentID.Valid {
		id := uint64(row.DepartmentID.Int64)
		dto.DepartmentID = &id
	}
	return dto
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
		out[i] = toListUserDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email        string  `json:"email"`
		Name         string  `json:"name"`
		Role         string  `json:"role"`
		DepartmentID *uint64 `json:"department_id"`
		Password     string  `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	role, ok := validRoles[req.Role]
	if req.Email == "" || req.Name == "" || !ok || len(req.Password) < 8 {
		http.Error(w, "email, name, a valid role (field_user/office_admin) and a password of at least 8 characters are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateUser(r.Context(), req.Email, req.Name, role, req.DepartmentID, req.Password)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityUser, uint64(id))
	writeJSON(w, http.StatusCreated, userResponse{ID: uint64(id), Email: req.Email, Name: req.Name, Role: req.Role, DepartmentID: req.DepartmentID})
}

func (s *Service) UpdateUserHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name         string  `json:"name"`
		Role         string  `json:"role"`
		DepartmentID *uint64 `json:"department_id"`
		IsActive     bool    `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	role, validRole := validRoles[req.Role]
	if req.Name == "" || !validRole {
		http.Error(w, "name and a valid role (field_user/office_admin) are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateUser(r.Context(), id, req.Name, role, req.DepartmentID, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityUser, id)
	writeJSON(w, http.StatusOK, userResponse{ID: id, Name: req.Name, Role: req.Role, DepartmentID: req.DepartmentID})
}

func (s *Service) ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if err := s.ResetPassword(r.Context(), id, req.Password); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityUser, id)
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	return httpx.PathID(w, r, "id")
}

// recordAudit is a best-effort audit log write: a failure here is logged but
// never fails the request.
func recordAudit(r *http.Request, q *db.Queries, action, entityType string, id uint64) {
	var userID *uint64
	if actor, ok := UserFromContext(r.Context()); ok {
		userID = &actor.ID
	}
	if err := audit.Record(r.Context(), q, audit.Params{
		UserID: userID, Action: action, EntityType: entityType, EntityID: &id,
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", action, "entity_type", entityType)
	}
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("auth handler error", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
