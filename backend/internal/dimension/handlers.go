package dimension

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

type businessDTO struct {
	ID       uint64 `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

type departmentDTO struct {
	ID       uint64 `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

type accountDTO struct {
	ID          uint64 `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	AccountType string `json:"account_type"`
	IsActive    bool   `json:"is_active"`
}

func toBusinessDTO(b db.DimBusiness) businessDTO {
	return businessDTO{ID: b.ID, Code: b.Code, Name: b.Name, IsActive: b.IsActive}
}

func toDepartmentDTO(d db.DimDepartment) departmentDTO {
	return departmentDTO{ID: d.ID, Code: d.Code, Name: d.Name, IsActive: d.IsActive}
}

func toAccountDTO(a db.DimAccount) accountDTO {
	return accountDTO{ID: a.ID, Code: a.Code, Name: a.Name, AccountType: string(a.AccountType), IsActive: a.IsActive}
}

type serviceDTO struct {
	ID         uint64 `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	BusinessID uint64 `json:"business_id"`
	IsActive   bool   `json:"is_active"`
}

type projectDTO struct {
	ID                  uint64 `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	ServiceID           uint64 `json:"service_id"`
	PrimaryDepartmentID uint64 `json:"primary_department_id"`
	IsActive            bool   `json:"is_active"`
}

type initiativeDTO struct {
	ID                  uint64 `json:"id"`
	Code                string `json:"code"`
	Name                string `json:"name"`
	ProjectID           uint64 `json:"project_id"`
	PrimaryDepartmentID uint64 `json:"primary_department_id"`
	IsActive            bool   `json:"is_active"`
}

func toServiceDTO(s db.DimService) serviceDTO {
	return serviceDTO{ID: s.ID, Code: s.Code, Name: s.Name, BusinessID: s.BusinessID, IsActive: s.IsActive}
}

func toProjectDTO(p db.DimProject) projectDTO {
	return projectDTO{ID: p.ID, Code: p.Code, Name: p.Name, ServiceID: p.ServiceID, PrimaryDepartmentID: p.PrimaryDepartmentID, IsActive: p.IsActive}
}

func toInitiativeDTO(i db.ListInitiativesRow) initiativeDTO {
	return initiativeDTO{
		ID: i.ID, Code: i.Code, Name: i.Name,
		ProjectID: i.ProjectID, PrimaryDepartmentID: i.PrimaryDepartmentID, IsActive: i.IsActive,
	}
}

// --- businesses ---

func (s *Service) ListBusinessesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListBusinesses(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]businessDTO, len(rows))
	for i, row := range rows {
		out[i] = toBusinessDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateBusinessHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		http.Error(w, "code and name are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateBusiness(r.Context(), req.Code, req.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityBusiness, uint64(id))
	writeJSON(w, http.StatusCreated, businessDTO{ID: uint64(id), Code: req.Code, Name: req.Name, IsActive: true})
}

func (s *Service) UpdateBusinessHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		http.Error(w, "code and name are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateBusiness(r.Context(), id, req.Code, req.Name, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityBusiness, id)
	writeJSON(w, http.StatusOK, businessDTO{ID: id, Code: req.Code, Name: req.Name, IsActive: req.IsActive})
}

// --- departments ---

func (s *Service) ListDepartmentsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListDepartments(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]departmentDTO, len(rows))
	for i, row := range rows {
		out[i] = toDepartmentDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		http.Error(w, "code and name are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateDepartment(r.Context(), req.Code, req.Name)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityDepartment, uint64(id))
	writeJSON(w, http.StatusCreated, departmentDTO{ID: uint64(id), Code: req.Code, Name: req.Name, IsActive: true})
}

func (s *Service) UpdateDepartmentHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" {
		http.Error(w, "code and name are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateDepartment(r.Context(), id, req.Code, req.Name, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityDepartment, id)
	writeJSON(w, http.StatusOK, departmentDTO{ID: id, Code: req.Code, Name: req.Name, IsActive: req.IsActive})
}

// --- accounts ---

var validAccountTypes = map[string]db.DimAccountAccountType{
	"revenue": db.DimAccountAccountTypeRevenue,
	"cost":    db.DimAccountAccountTypeCost,
	"other":   db.DimAccountAccountTypeOther,
}

func (s *Service) ListAccountsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListAccounts(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]accountDTO, len(rows))
	for i, row := range rows {
		out[i] = toAccountDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateAccountHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		AccountType string `json:"account_type"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	accountType, ok := validAccountTypes[req.AccountType]
	if req.Code == "" || req.Name == "" || !ok {
		http.Error(w, "code, name and a valid account_type (revenue/cost/other) are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateAccount(r.Context(), req.Code, req.Name, accountType)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityAccount, uint64(id))
	writeJSON(w, http.StatusCreated, accountDTO{ID: uint64(id), Code: req.Code, Name: req.Name, AccountType: req.AccountType, IsActive: true})
}

func (s *Service) UpdateAccountHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		AccountType string `json:"account_type"`
		IsActive    bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	accountType, validType := validAccountTypes[req.AccountType]
	if req.Code == "" || req.Name == "" || !validType {
		http.Error(w, "code, name and a valid account_type (revenue/cost/other) are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateAccount(r.Context(), id, req.Code, req.Name, accountType, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityAccount, id)
	writeJSON(w, http.StatusOK, accountDTO{ID: id, Code: req.Code, Name: req.Name, AccountType: req.AccountType, IsActive: req.IsActive})
}

// --- services ---

func (s *Service) ListServicesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListServices(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]serviceDTO, len(rows))
	for i, row := range rows {
		out[i] = toServiceDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateServiceHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		BusinessID uint64 `json:"business_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.BusinessID == 0 {
		http.Error(w, "code, name and business_id are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateService(r.Context(), req.Code, req.Name, req.BusinessID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityService, uint64(id))
	writeJSON(w, http.StatusCreated, serviceDTO{ID: uint64(id), Code: req.Code, Name: req.Name, BusinessID: req.BusinessID, IsActive: true})
}

func (s *Service) UpdateServiceHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		BusinessID uint64 `json:"business_id"`
		IsActive   bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.BusinessID == 0 {
		http.Error(w, "code, name and business_id are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateService(r.Context(), id, req.Code, req.Name, req.BusinessID, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityService, id)
	writeJSON(w, http.StatusOK, serviceDTO{ID: id, Code: req.Code, Name: req.Name, BusinessID: req.BusinessID, IsActive: req.IsActive})
}

// --- projects ---

func (s *Service) ListProjectsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListProjects(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]projectDTO, len(rows))
	for i, row := range rows {
		out[i] = toProjectDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code                string `json:"code"`
		Name                string `json:"name"`
		ServiceID           uint64 `json:"service_id"`
		PrimaryDepartmentID uint64 `json:"primary_department_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.ServiceID == 0 || req.PrimaryDepartmentID == 0 {
		http.Error(w, "code, name, service_id and primary_department_id are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateProject(r.Context(), req.Code, req.Name, req.ServiceID, req.PrimaryDepartmentID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityProject, uint64(id))
	writeJSON(w, http.StatusCreated, projectDTO{
		ID: uint64(id), Code: req.Code, Name: req.Name,
		ServiceID: req.ServiceID, PrimaryDepartmentID: req.PrimaryDepartmentID, IsActive: true,
	})
}

func (s *Service) UpdateProjectHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code                string `json:"code"`
		Name                string `json:"name"`
		ServiceID           uint64 `json:"service_id"`
		PrimaryDepartmentID uint64 `json:"primary_department_id"`
		IsActive            bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.ServiceID == 0 || req.PrimaryDepartmentID == 0 {
		http.Error(w, "code, name, service_id and primary_department_id are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateProject(r.Context(), id, req.Code, req.Name, req.ServiceID, req.PrimaryDepartmentID, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityProject, id)
	writeJSON(w, http.StatusOK, projectDTO{
		ID: id, Code: req.Code, Name: req.Name,
		ServiceID: req.ServiceID, PrimaryDepartmentID: req.PrimaryDepartmentID, IsActive: req.IsActive,
	})
}

// --- initiatives ---

func (s *Service) ListInitiativesHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.ListInitiatives(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]initiativeDTO, len(rows))
	for i, row := range rows {
		out[i] = toInitiativeDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateInitiativeHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code                string `json:"code"`
		Name                string `json:"name"`
		ProjectID           uint64 `json:"project_id"`
		PrimaryDepartmentID uint64 `json:"primary_department_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.ProjectID == 0 || req.PrimaryDepartmentID == 0 {
		http.Error(w, "code, name, project_id and primary_department_id are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateInitiative(r.Context(), req.Code, req.Name, req.ProjectID, req.PrimaryDepartmentID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionCreate, audit.EntityInitiative, uint64(id))
	writeJSON(w, http.StatusCreated, initiativeDTO{
		ID: uint64(id), Code: req.Code, Name: req.Name,
		ProjectID: req.ProjectID, PrimaryDepartmentID: req.PrimaryDepartmentID, IsActive: true,
	})
}

func (s *Service) UpdateInitiativeHandler(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Code                string `json:"code"`
		Name                string `json:"name"`
		ProjectID           uint64 `json:"project_id"`
		PrimaryDepartmentID uint64 `json:"primary_department_id"`
		IsActive            bool   `json:"is_active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.Name == "" || req.ProjectID == 0 || req.PrimaryDepartmentID == 0 {
		http.Error(w, "code, name, project_id and primary_department_id are required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateInitiative(r.Context(), id, req.Code, req.Name, req.ProjectID, req.PrimaryDepartmentID, req.IsActive); err != nil {
		writeInternalError(w, err)
		return
	}
	recordAudit(r, s.Queries, audit.ActionUpdate, audit.EntityInitiative, id)
	writeJSON(w, http.StatusOK, initiativeDTO{
		ID: id, Code: req.Code, Name: req.Name,
		ProjectID: req.ProjectID, PrimaryDepartmentID: req.PrimaryDepartmentID, IsActive: req.IsActive,
	})
}

// --- shared helpers ---

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("dimension handler error", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// recordAudit is a best-effort audit log write: a failure here is logged
// but never fails the request — a transient audit_log write error shouldn't
// block routine master-data maintenance.
func recordAudit(r *http.Request, q *db.Queries, action, entityType string, id uint64) {
	var userID *uint64
	if actor, ok := auth.UserFromContext(r.Context()); ok {
		userID = &actor.ID
	}
	if err := audit.Record(r.Context(), q, audit.Params{
		UserID: userID, Action: action, EntityType: entityType, EntityID: &id,
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", action, "entity_type", entityType)
	}
}
