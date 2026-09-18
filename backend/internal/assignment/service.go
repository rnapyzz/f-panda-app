// Package assignment tracks which field user is responsible for which
// business×department scope (user_assignment), and — by joining that
// against submission_scope — answers "who hasn't submitted yet" for the
// Phase 4 submission-status dashboard.
package assignment

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

func (s *Service) ListActive(ctx context.Context) ([]db.ListActiveUserAssignmentsRow, error) {
	return s.Queries.ListActiveUserAssignments(ctx)
}

func (s *Service) Create(ctx context.Context, userID, businessID, departmentID uint64) (int64, error) {
	return s.Queries.CreateUserAssignment(ctx, db.CreateUserAssignmentParams{
		UserID: userID, BusinessID: businessID, DepartmentID: departmentID,
	})
}

func (s *Service) Deactivate(ctx context.Context, id uint64) error {
	return s.Queries.DeactivateUserAssignment(ctx, id)
}
