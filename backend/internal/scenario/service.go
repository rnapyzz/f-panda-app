// Package scenario manages scenario_version rows: the "vintage" concept
// that lets budget revisions and monthly rolling-forecast snapshots coexist
// without colliding, each addressed by (scenario_type, fiscal_year,
// as_of_period for forecasts) rather than being silently overwritten.
package scenario

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

var ErrForecastRequiresAsOfPeriod = errors.New("scenario: forecast versions require as_of_period_id")
var ErrAsOfPeriodOnlyForForecast = errors.New("scenario: as_of_period_id is only valid for forecast versions")

type Service struct {
	DB      *sql.DB
	Queries *db.Queries
}

func NewService(sqlDB *sql.DB, q *db.Queries) *Service {
	return &Service{DB: sqlDB, Queries: q}
}

type CreateInput struct {
	ScenarioType db.ScenarioVersionScenarioType
	FiscalYear   int16
	AsOfPeriodID *uint64
	VersionLabel string
	CreatedBy    uint64
}

// Create opens a new scenario version and marks it the current one for its
// (scenario_type, fiscal_year), atomically demoting whatever was current
// before it. This is what makes "the current forecast" a stable thing other
// queries can join against even as new monthly forecast vintages are added.
func (s *Service) Create(ctx context.Context, in CreateInput) (int64, error) {
	if in.ScenarioType == db.ScenarioVersionScenarioTypeForecast && in.AsOfPeriodID == nil {
		return 0, ErrForecastRequiresAsOfPeriod
	}
	if in.ScenarioType != db.ScenarioVersionScenarioTypeForecast && in.AsOfPeriodID != nil {
		return 0, ErrAsOfPeriodOnlyForForecast
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	qtx := s.Queries.WithTx(tx)

	if err := qtx.UnsetCurrentScenarioVersions(ctx, db.UnsetCurrentScenarioVersionsParams{
		ScenarioType: in.ScenarioType,
		FiscalYear:   in.FiscalYear,
	}); err != nil {
		return 0, err
	}

	var asOfPeriodID sql.NullInt64
	if in.AsOfPeriodID != nil {
		asOfPeriodID = sql.NullInt64{Int64: int64(*in.AsOfPeriodID), Valid: true}
	}

	id, err := qtx.CreateScenarioVersion(ctx, db.CreateScenarioVersionParams{
		ScenarioType: in.ScenarioType,
		FiscalYear:   in.FiscalYear,
		AsOfPeriodID: asOfPeriodID,
		VersionLabel: in.VersionLabel,
		CreatedBy:    in.CreatedBy,
	})
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) List(ctx context.Context, scenarioType db.ScenarioVersionScenarioType, fiscalYear int16) ([]db.ScenarioVersion, error) {
	return s.Queries.ListScenarioVersions(ctx, db.ListScenarioVersionsParams{
		ScenarioType: scenarioType,
		FiscalYear:   fiscalYear,
	})
}

// Current returns the current version for (scenarioType, fiscalYear), or
// (nil, nil) if none has been created yet — a normal state, not an error,
// since a fiscal year's budget/forecast/actual cycle starts out empty.
func (s *Service) Current(ctx context.Context, scenarioType db.ScenarioVersionScenarioType, fiscalYear int16) (*db.ScenarioVersion, error) {
	v, err := s.Queries.GetCurrentScenarioVersion(ctx, db.GetCurrentScenarioVersionParams{
		ScenarioType: scenarioType,
		FiscalYear:   fiscalYear,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get current scenario version: %w", err)
	}
	return &v, nil
}
