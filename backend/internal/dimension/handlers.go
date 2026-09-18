package dimension

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
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
	writeJSON(w, http.StatusOK, accountDTO{ID: id, Code: req.Code, Name: req.Name, AccountType: req.AccountType, IsActive: req.IsActive})
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
