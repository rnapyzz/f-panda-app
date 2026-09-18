// Package inputsheet implements the free-form spreadsheet input layer: each
// field user's personal sheet, the "cell binding" that maps a range of it
// onto the canonical dimension schema, and the submission pipeline that
// turns bound cells into fact_amount rows. This is the differentiating
// feature described in docs/plan.md — 事務局 owns the fixed schema, field
// users keep their own layout, and a binding bridges the two.
package inputsheet

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type Service struct {
	DB      *sql.DB
	Queries *db.Queries
}

func NewService(sqlDB *sql.DB, q *db.Queries) *Service {
	return &Service{DB: sqlDB, Queries: q}
}

func (s *Service) CreateSheet(ctx context.Context, ownerID uint64, name string, snapshot json.RawMessage) (int64, error) {
	return s.Queries.CreateInputSheet(ctx, db.CreateInputSheetParams{
		OwnerUserID:   ownerID,
		Name:          name,
		SheetSnapshot: string(snapshot),
	})
}

func (s *Service) UpdateSheetSnapshot(ctx context.Context, sheetID, ownerID uint64, snapshot json.RawMessage) error {
	return s.Queries.UpdateInputSheetSnapshot(ctx, db.UpdateInputSheetSnapshotParams{
		SheetSnapshot: string(snapshot),
		ID:            sheetID,
		OwnerUserID:   ownerID,
	})
}

func (s *Service) GetSheet(ctx context.Context, id uint64) (db.InputSheet, error) {
	return s.Queries.GetInputSheet(ctx, id)
}

func (s *Service) ListSheetsByOwner(ctx context.Context, ownerID uint64) ([]db.ListInputSheetsByOwnerRow, error) {
	return s.Queries.ListInputSheetsByOwner(ctx, ownerID)
}
