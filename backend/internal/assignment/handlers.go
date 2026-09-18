package assignment

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

type assignmentDTO struct {
	ID             uint64 `json:"id"`
	UserID         uint64 `json:"user_id"`
	UserName       string `json:"user_name"`
	UserEmail      string `json:"user_email"`
	BusinessID     uint64 `json:"business_id"`
	BusinessCode   string `json:"business_code"`
	BusinessName   string `json:"business_name"`
	DepartmentID   uint64 `json:"department_id"`
	DepartmentCode string `json:"department_code"`
	DepartmentName string `json:"department_name"`
}

func toAssignmentDTO(row db.ListActiveUserAssignmentsRow) assignmentDTO {
	return assignmentDTO{
		ID: row.ID, UserID: row.UserID, UserName: row.UserName, UserEmail: row.UserEmail,
		BusinessID: row.BusinessID, BusinessCode: row.BusinessCode, BusinessName: row.BusinessName,
		DepartmentID: row.DepartmentID, DepartmentCode: row.DepartmentCode, DepartmentName: row.DepartmentName,
	}
}

func (s *Service) ListAssignmentsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListActive(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]assignmentDTO, len(rows))
	for i, row := range rows {
		out[i] = toAssignmentDTO(row)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) CreateAssignmentHandler(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		UserID       uint64 `json:"user_id"`
		BusinessID   uint64 `json:"business_id"`
		DepartmentID uint64 `json:"department_id"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.UserID == 0 || req.BusinessID == 0 || req.DepartmentID == 0 {
		http.Error(w, "user_id, business_id and department_id are required", http.StatusBadRequest)
		return
	}

	id, err := s.Create(r.Context(), req.UserID, req.BusinessID, req.DepartmentID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	eid := uint64(id)
	if err := audit.Record(r.Context(), s.Queries, audit.Params{
		UserID: &actor.ID, Action: audit.ActionCreate, EntityType: audit.EntityUserAssignment, EntityID: &eid,
		Detail:    map[string]uint64{"user_id": req.UserID, "business_id": req.BusinessID, "department_id": req.DepartmentID},
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", audit.ActionCreate, "entity_type", audit.EntityUserAssignment)
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]uint64{"id": eid})
}

func (s *Service) DeactivateAssignmentHandler(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, ok := httpx.PathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.Deactivate(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}

	if err := audit.Record(r.Context(), s.Queries, audit.Params{
		UserID: &actor.ID, Action: audit.ActionUpdate, EntityType: audit.EntityUserAssignment, EntityID: &id,
		Detail:    map[string]string{"change": "deactivated"},
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", audit.ActionUpdate, "entity_type", audit.EntityUserAssignment)
	}

	w.WriteHeader(http.StatusNoContent)
}

type scopeStatusDTO struct {
	BusinessID       uint64         `json:"business_id"`
	BusinessCode     string         `json:"business_code"`
	BusinessName     string         `json:"business_name"`
	DepartmentID     uint64         `json:"department_id"`
	DepartmentCode   string         `json:"department_code"`
	DepartmentName   string         `json:"department_name"`
	AssignedUsers    []assignedUser `json:"assigned_users"`
	Submitted        bool           `json:"submitted"`
	SubmissionID     *uint64        `json:"submission_id,omitempty"`
	ValidationStatus *string        `json:"validation_status,omitempty"`
	SubmittedAt      *string        `json:"submitted_at,omitempty"`
}

type assignedUser struct {
	UserID uint64 `json:"user_id"`
	Name   string `json:"name"`
}

func toScopeStatusDTO(st ScopeStatus) scopeStatusDTO {
	users := make([]assignedUser, len(st.AssignedUsers))
	for i, u := range st.AssignedUsers {
		users[i] = assignedUser{UserID: u.UserID, Name: u.Name}
	}
	dto := scopeStatusDTO{
		BusinessID: st.BusinessID, BusinessCode: st.BusinessCode, BusinessName: st.BusinessName,
		DepartmentID: st.DepartmentID, DepartmentCode: st.DepartmentCode, DepartmentName: st.DepartmentName,
		AssignedUsers: users, Submitted: st.Submitted,
		SubmissionID: st.SubmissionID, ValidationStatus: st.ValidationStatus,
	}
	if st.SubmittedAt != nil {
		s := st.SubmittedAt.Format(time.RFC3339)
		dto.SubmittedAt = &s
	}
	return dto
}

func (s *Service) SubmissionStatusHandler(w http.ResponseWriter, r *http.Request) {
	scenarioVersionID, err := strconv.ParseUint(r.URL.Query().Get("scenario_version_id"), 10, 64)
	if err != nil {
		http.Error(w, "scenario_version_id query parameter is required", http.StatusBadRequest)
		return
	}

	rows, err := s.SubmissionStatus(r.Context(), scenarioVersionID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]scopeStatusDTO, len(rows))
	for i, row := range rows {
		out[i] = toScopeStatusDTO(row)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("assignment handler error", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
