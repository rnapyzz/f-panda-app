package assignment

import (
	"context"
	"time"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type scopeKey struct {
	businessID   uint64
	departmentID uint64
}

type AssignedUser struct {
	UserID uint64
	Name   string
}

type ScopeStatus struct {
	BusinessID       uint64
	BusinessCode     string
	BusinessName     string
	DepartmentID     uint64
	DepartmentCode   string
	DepartmentName   string
	AssignedUsers    []AssignedUser
	Submitted        bool
	SubmissionID     *uint64
	ValidationStatus *string
	SubmittedAt      *time.Time
}

// SubmissionStatus answers, for every business×department scope that has an
// active assignment, whether it has a non-superseded submission against
// scenarioVersionID yet — the core query behind the Phase 4 submission
// dashboard. Scopes with no assignment at all aren't included: the
// dashboard is about tracking assigned work, not every theoretical scope.
func (s *Service) SubmissionStatus(ctx context.Context, scenarioVersionID uint64) ([]ScopeStatus, error) {
	assignments, err := s.Queries.ListActiveUserAssignments(ctx)
	if err != nil {
		return nil, err
	}
	scopes, err := s.Queries.ListSubmissionScopesByScenarioVersion(ctx, scenarioVersionID)
	if err != nil {
		return nil, err
	}

	// scopes is ordered submitted_at DESC, so the first row seen per key is
	// the latest non-superseded submission covering that scope.
	latest := map[scopeKey]db.ListSubmissionScopesByScenarioVersionRow{}
	for _, sc := range scopes {
		key := scopeKey{businessID: sc.BusinessID, departmentID: sc.DepartmentID}
		if _, seen := latest[key]; !seen {
			latest[key] = sc
		}
	}

	order := []scopeKey{}
	statuses := map[scopeKey]*ScopeStatus{}
	for _, a := range assignments {
		key := scopeKey{businessID: a.BusinessID, departmentID: a.DepartmentID}
		status, ok := statuses[key]
		if !ok {
			status = &ScopeStatus{
				BusinessID: a.BusinessID, BusinessCode: a.BusinessCode, BusinessName: a.BusinessName,
				DepartmentID: a.DepartmentID, DepartmentCode: a.DepartmentCode, DepartmentName: a.DepartmentName,
			}
			statuses[key] = status
			order = append(order, key)
		}
		status.AssignedUsers = append(status.AssignedUsers, AssignedUser{UserID: a.UserID, Name: a.UserName})
	}

	for _, key := range order {
		status := statuses[key]
		if sub, ok := latest[key]; ok {
			status.Submitted = true
			id := sub.SubmissionID
			status.SubmissionID = &id
			vs := string(sub.ValidationStatus)
			status.ValidationStatus = &vs
			t := sub.SubmittedAt
			status.SubmittedAt = &t
		}
	}

	out := make([]ScopeStatus, len(order))
	for i, key := range order {
		out[i] = *statuses[key]
	}
	return out, nil
}
