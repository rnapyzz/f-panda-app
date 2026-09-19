// Package audit records who did what to which entity, backing the
// office-admin audit trail viewer (Phase 4). Record is a free function
// rather than a method on a stateful type so any package's
// transaction-scoped *db.Queries (via Queries.WithTx(tx)) can log
// atomically alongside the write it documents.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

// action/entity_type are plain strings rather than DB ENUMs so a new kind of
// operation never requires a schema migration. These constants exist only
// to keep call sites consistent.
const (
	ActionLogin  = "login"
	ActionLogout = "logout"
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionSubmit = "submit"
	ActionImport = "import_commit"
)

const (
	EntityUser            = "app_user"
	EntityBusiness        = "dim_business"
	EntityDepartment      = "dim_department"
	EntityAccount         = "dim_account"
	EntityService         = "dim_service"
	EntityProject         = "dim_project"
	EntityInitiative      = "dim_initiative"
	EntityScenarioVersion = "scenario_version"
	EntityInputSheet      = "input_sheet"
	EntityInputBinding    = "input_binding"
	EntitySubmission      = "submission"
	EntityImportBatch     = "import_batch"
	EntityUserAssignment  = "user_assignment"
)

type Params struct {
	UserID     *uint64
	Action     string
	EntityType string
	EntityID   *uint64
	Detail     any // marshaled to JSON; nil becomes the JSON literal "null"
	IPAddress  string
}

// Record inserts one audit_log row via q — pass a package-level *db.Queries
// for a standalone call, or Queries.WithTx(tx) to log atomically with the
// write it documents.
func Record(ctx context.Context, q *db.Queries, p Params) error {
	// Always write a valid JSON value, never a SQL NULL: database/sql cannot
	// Scan a NULL column back into *json.RawMessage, so a later read (e.g.
	// ListAuditLogs) would fail with "unsupported Scan ... into
	// *json.RawMessage". Same rule already applied to submission's
	// validation_detail and import_batch's error_detail.
	detail := json.RawMessage("null")
	if p.Detail != nil {
		marshaled, err := json.Marshal(p.Detail)
		if err != nil {
			return err
		}
		detail = marshaled
	}

	var userID sql.NullInt64
	if p.UserID != nil {
		userID = sql.NullInt64{Int64: int64(*p.UserID), Valid: true}
	}
	var entityID sql.NullInt64
	if p.EntityID != nil {
		entityID = sql.NullInt64{Int64: int64(*p.EntityID), Valid: true}
	}

	return q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		UserID:     userID,
		Action:     p.Action,
		EntityType: p.EntityType,
		EntityID:   entityID,
		Detail:     detail,
		IpAddress:  sql.NullString{String: p.IPAddress, Valid: p.IPAddress != ""},
	})
}

type Service struct {
	Queries *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{Queries: q}
}

type ListFilter struct {
	UserID     *uint64
	Action     string
	EntityType string
	FromDate   *time.Time
	ToDate     *time.Time
	Limit      int32
	Offset     int32
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]db.ListAuditLogsRow, error) {
	arg := db.ListAuditLogsParams{
		Limit:  f.Limit,
		Offset: f.Offset,
	}
	if f.UserID != nil {
		arg.UserID = sql.NullInt64{Int64: int64(*f.UserID), Valid: true}
	}
	if f.Action != "" {
		arg.Action = sql.NullString{String: f.Action, Valid: true}
	}
	if f.EntityType != "" {
		arg.EntityType = sql.NullString{String: f.EntityType, Valid: true}
	}
	if f.FromDate != nil {
		arg.FromDate = sql.NullTime{Time: *f.FromDate, Valid: true}
	}
	if f.ToDate != nil {
		arg.ToDate = sql.NullTime{Time: *f.ToDate, Valid: true}
	}
	return s.Queries.ListAuditLogs(ctx, arg)
}
