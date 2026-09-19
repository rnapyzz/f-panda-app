// Package dimension implements office-admin CRUD for the fixed master data
// (business, department, account) that the input layer's cell bindings will
// eventually resolve free-form sheet ranges against.
package dimension

import (
	"context"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type Service struct {
	Queries *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{Queries: q}
}

func (s *Service) ListBusinesses(ctx context.Context) ([]db.DimBusiness, error) {
	return s.Queries.ListBusinesses(ctx)
}

func (s *Service) CreateBusiness(ctx context.Context, code, name string) (int64, error) {
	return s.Queries.CreateBusiness(ctx, db.CreateBusinessParams{Code: code, Name: name})
}

func (s *Service) UpdateBusiness(ctx context.Context, id uint64, code, name string, isActive bool) error {
	return s.Queries.UpdateBusiness(ctx, db.UpdateBusinessParams{
		ID: id, Code: code, Name: name, IsActive: isActive,
	})
}

func (s *Service) ListDepartments(ctx context.Context) ([]db.DimDepartment, error) {
	return s.Queries.ListDepartments(ctx)
}

func (s *Service) CreateDepartment(ctx context.Context, code, name string) (int64, error) {
	return s.Queries.CreateDepartment(ctx, db.CreateDepartmentParams{Code: code, Name: name})
}

func (s *Service) UpdateDepartment(ctx context.Context, id uint64, code, name string, isActive bool) error {
	return s.Queries.UpdateDepartment(ctx, db.UpdateDepartmentParams{
		ID: id, Code: code, Name: name, IsActive: isActive,
	})
}

func (s *Service) ListAccounts(ctx context.Context) ([]db.DimAccount, error) {
	return s.Queries.ListAccounts(ctx)
}

func (s *Service) CreateAccount(ctx context.Context, code, name string, accountType db.DimAccountAccountType) (int64, error) {
	return s.Queries.CreateAccount(ctx, db.CreateAccountParams{Code: code, Name: name, AccountType: accountType})
}

func (s *Service) UpdateAccount(ctx context.Context, id uint64, code, name string, accountType db.DimAccountAccountType, isActive bool) error {
	return s.Queries.UpdateAccount(ctx, db.UpdateAccountParams{
		ID: id, Code: code, Name: name, AccountType: accountType, IsActive: isActive,
	})
}

func (s *Service) ListServices(ctx context.Context) ([]db.DimService, error) {
	return s.Queries.ListServices(ctx)
}

func (s *Service) CreateService(ctx context.Context, code, name string, businessID uint64) (int64, error) {
	return s.Queries.CreateService(ctx, db.CreateServiceParams{Code: code, Name: name, BusinessID: businessID})
}

func (s *Service) UpdateService(ctx context.Context, id uint64, code, name string, businessID uint64, isActive bool) error {
	return s.Queries.UpdateService(ctx, db.UpdateServiceParams{
		ID: id, Code: code, Name: name, BusinessID: businessID, IsActive: isActive,
	})
}

func (s *Service) ListProjects(ctx context.Context) ([]db.DimProject, error) {
	return s.Queries.ListProjects(ctx)
}

func (s *Service) CreateProject(ctx context.Context, code, name string, serviceID, primaryDepartmentID uint64) (int64, error) {
	return s.Queries.CreateProject(ctx, db.CreateProjectParams{
		Code: code, Name: name, ServiceID: serviceID, PrimaryDepartmentID: primaryDepartmentID,
	})
}

func (s *Service) UpdateProject(ctx context.Context, id uint64, code, name string, serviceID, primaryDepartmentID uint64, isActive bool) error {
	return s.Queries.UpdateProject(ctx, db.UpdateProjectParams{
		ID: id, Code: code, Name: name, ServiceID: serviceID, PrimaryDepartmentID: primaryDepartmentID, IsActive: isActive,
	})
}

// ListInitiatives returns ListInitiativesRow (not the DimInitiative model
// type): the sqlc schema simulator appends a renamed column (service_id ->
// project_id, via migration 00009's ALTER TABLE ... CHANGE COLUMN) to the
// end of its virtual table definition rather than preserving its original
// position, so a SELECT listing project_id in its natural column order no
// longer matches DimInitiative's field order closely enough to alias to it.
func (s *Service) ListInitiatives(ctx context.Context) ([]db.ListInitiativesRow, error) {
	return s.Queries.ListInitiatives(ctx)
}

func (s *Service) CreateInitiative(ctx context.Context, code, name string, projectID, primaryDepartmentID uint64) (int64, error) {
	return s.Queries.CreateInitiative(ctx, db.CreateInitiativeParams{
		Code: code, Name: name, ProjectID: projectID, PrimaryDepartmentID: primaryDepartmentID,
	})
}

func (s *Service) UpdateInitiative(ctx context.Context, id uint64, code, name string, projectID, primaryDepartmentID uint64, isActive bool) error {
	return s.Queries.UpdateInitiative(ctx, db.UpdateInitiativeParams{
		ID: id, Code: code, Name: name, ProjectID: projectID, PrimaryDepartmentID: primaryDepartmentID, IsActive: isActive,
	})
}
