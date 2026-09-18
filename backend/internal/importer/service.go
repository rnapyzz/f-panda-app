package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type Service struct {
	DB      *sql.DB
	Queries *db.Queries
}

func NewService(sqlDB *sql.DB, q *db.Queries) *Service {
	return &Service{DB: sqlDB, Queries: q}
}

var ErrScenarioNotActual = errors.New("importer: scenario_version must be of type 'actual'")

const previewSampleRows = 10

type PreviewResult struct {
	Headers    []string
	SampleRows [][]string
	TotalRows  int
}

// Preview parses the file and returns just enough to drive the column
// mapping wizard — it never touches the database.
func Preview(filename string, content []byte) (PreviewResult, error) {
	table, err := DetectAndParse(filename, content)
	if err != nil {
		return PreviewResult{}, err
	}
	sample := table.Rows
	if len(sample) > previewSampleRows {
		sample = sample[:previewSampleRows]
	}
	return PreviewResult{Headers: table.Headers, SampleRows: sample, TotalRows: len(table.Rows)}, nil
}

type CommitInput struct {
	UploadedBy        uint64
	OriginalFilename  string
	FileSizeBytes     int
	ScenarioVersionID uint64
	Content           []byte
	Mapping           ColumnMapping
}

type CommitResult struct {
	ImportBatchID int64
	Status        string
	RowCount      int
	ErrorCount    int
	Errors        []RowError
}

// Commit parses the file, resolves every row against the current dimension
// masters, and — in one transaction — records an import_batch and writes a
// fact_amount row (source_type='csv_import') for every resolved dimension
// combination. Rows that fail to resolve don't abort the batch; they're
// counted and reported back, and the rest of the file still imports.
func (s *Service) Commit(ctx context.Context, in CommitInput) (CommitResult, error) {
	version, err := s.Queries.GetScenarioVersionByID(ctx, in.ScenarioVersionID)
	if err != nil {
		return CommitResult{}, fmt.Errorf("load scenario version: %w", err)
	}
	if version.ScenarioType != db.ScenarioVersionScenarioTypeActual {
		return CommitResult{}, ErrScenarioNotActual
	}

	table, err := DetectAndParse(in.OriginalFilename, in.Content)
	if err != nil {
		return CommitResult{}, err
	}

	businesses, err := s.Queries.ListBusinesses(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	departments, err := s.Queries.ListDepartments(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	accounts, err := s.Queries.ListAccounts(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	periods, err := s.Queries.ListPeriods(ctx)
	if err != nil {
		return CommitResult{}, err
	}

	resolved := resolveRows(table, in.Mapping, businesses, departments, accounts, periods)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return CommitResult{}, err
	}
	defer tx.Rollback()
	qtx := s.Queries.WithTx(tx)

	batchID, err := qtx.CreateImportBatch(ctx, db.CreateImportBatchParams{
		UploadedBy:        in.UploadedBy,
		OriginalFilename:  in.OriginalFilename,
		FileSizeBytes:     int32(in.FileSizeBytes),
		ScenarioVersionID: in.ScenarioVersionID,
	})
	if err != nil {
		return CommitResult{}, fmt.Errorf("create import batch: %w", err)
	}

	for key, amount := range resolved.Amounts {
		if err := qtx.UpsertFactAmountFromImport(ctx, db.UpsertFactAmountFromImportParams{
			ScenarioVersionID: in.ScenarioVersionID,
			BusinessID:        key.businessID,
			DepartmentID:      key.departmentID,
			AccountID:         key.accountID,
			PeriodID:          key.periodID,
			Amount:            formatAmount(amount),
			ImportBatchID:     sql.NullInt64{Int64: batchID, Valid: true},
			CreatedBy:         in.UploadedBy,
		}); err != nil {
			return CommitResult{}, fmt.Errorf("write fact amount: %w", err)
		}
	}

	status := db.ImportBatchStatusCompleted
	if len(resolved.Amounts) == 0 && resolved.TotalErrors > 0 {
		status = db.ImportBatchStatusFailed
	}

	// Always write a valid JSON value (never a SQL NULL): database/sql
	// cannot Scan a NULL column back into *json.RawMessage (it only
	// implements a plain byte slice, not sql.Scanner), so a later read
	// would fail with "unsupported Scan ... into *json.RawMessage".
	errorDetail := json.RawMessage("null")
	if len(resolved.Errors) > 0 {
		errorDetail, err = json.Marshal(resolved.Errors)
		if err != nil {
			return CommitResult{}, err
		}
	}

	if err := qtx.CompleteImportBatch(ctx, db.CompleteImportBatchParams{
		Status:      status,
		RowCount:    int32(len(resolved.Amounts)),
		ErrorCount:  int32(resolved.TotalErrors),
		ErrorDetail: errorDetail,
		ID:          uint64(batchID),
	}); err != nil {
		return CommitResult{}, fmt.Errorf("complete import batch: %w", err)
	}

	bid := uint64(batchID)
	if err := audit.Record(ctx, qtx, audit.Params{
		UserID: &in.UploadedBy, Action: audit.ActionImport, EntityType: audit.EntityImportBatch, EntityID: &bid,
		Detail: map[string]any{
			"original_filename": in.OriginalFilename, "scenario_version_id": in.ScenarioVersionID,
			"status": string(status), "row_count": len(resolved.Amounts), "error_count": resolved.TotalErrors,
		},
	}); err != nil {
		return CommitResult{}, fmt.Errorf("write audit log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return CommitResult{}, err
	}

	return CommitResult{
		ImportBatchID: batchID,
		Status:        string(status),
		RowCount:      len(resolved.Amounts),
		ErrorCount:    resolved.TotalErrors,
		Errors:        resolved.Errors,
	}, nil
}

func (s *Service) ListBatches(ctx context.Context, scenarioVersionID uint64) ([]db.ImportBatch, error) {
	return s.Queries.ListImportBatchesByScenarioVersion(ctx, scenarioVersionID)
}

func formatAmount(v float64) string {
	return fmt.Sprintf("%.2f", v)
}
