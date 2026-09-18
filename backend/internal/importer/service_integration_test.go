package importer_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/importer"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("APP_DB_DSN")
	if dsn == "" {
		t.Skip("APP_DB_DSN not set; skipping integration test (see README.md)")
	}
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.PingContext(context.Background()); err != nil {
		t.Skipf("cannot reach test database: %v", err)
	}
	return sqlDB
}

// TestCommitWritesActualsAndReflectsInVarianceReport is the Phase 3
// verification fixture from docs/plan.md: uploading a sample accounting
// export produces correct `actual` fact rows that show up in the variance
// report, a row that can't be resolved against the masters is reported as
// an error without aborting the rest of the batch, and re-importing with
// corrected amounts UPSERTs cleanly.
func TestCommitWritesActualsAndReflectsInVarianceReport(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	scenarios := scenario.NewService(sqlDB, queries)
	facts := fact.NewService(queries, scenarios)
	importerSvc := importer.NewService(sqlDB, queries)

	runID := time.Now().UnixNano()
	const fiscalYear = 9003 // sentinel FY unlikely to collide with real data

	adminID, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email: fmt.Sprintf("importer-test-%d@example.com", runID), Name: "Test Admin",
		Role: db.AppUserRoleOfficeAdmin, PasswordHash: "unused",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uint64(adminID)

	// Names (not just codes) are suffixed with runID too: the importer
	// resolves rows by exact code-OR-name match, so a non-unique name would
	// collide with leftover fixtures from other test runs sharing this dev
	// database and silently resolve to the wrong (older) row.
	businessName := fmt.Sprintf("Test Biz %d", runID)
	departmentName := fmt.Sprintf("Test Dept %d", runID)
	accountName := fmt.Sprintf("人件費%d", runID)

	businessIDRaw, err := queries.CreateBusiness(ctx, db.CreateBusinessParams{Code: fmt.Sprintf("IMB-%d", runID), Name: businessName})
	if err != nil {
		t.Fatalf("create business: %v", err)
	}
	businessID := uint64(businessIDRaw)
	if _, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{Code: fmt.Sprintf("IMD-%d", runID), Name: departmentName}); err != nil {
		t.Fatalf("create department: %v", err)
	}
	accountID, err := queries.CreateAccount(ctx, db.CreateAccountParams{Code: fmt.Sprintf("IMA-%d", runID), Name: accountName, AccountType: db.DimAccountAccountTypeCost})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	if err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
		FiscalYear: fiscalYear, FiscalMonth: 1, CalendarYear: fiscalYear, CalendarMonth: 4,
		StartDate: time.Date(fiscalYear, 4, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(fiscalYear, 4, 30, 0, 0, 0, 0, time.UTC),
		Label:     fmt.Sprintf("FY%d-IM-04", fiscalYear),
	}); err != nil {
		t.Fatalf("upsert period: %v", err)
	}
	periodRows, err := queries.ListPeriods(ctx)
	if err != nil {
		t.Fatalf("list periods: %v", err)
	}
	var periodID uint64
	for _, p := range periodRows {
		if p.FiscalYear == fiscalYear && p.CalendarMonth == 4 {
			periodID = p.ID
		}
	}
	if periodID == 0 {
		t.Fatal("fixture period not found after upsert")
	}

	actualVersionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeActual, FiscalYear: fiscalYear,
		VersionLabel: fmt.Sprintf("test actual %d", runID), CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create actual scenario version: %v", err)
	}
	budgetVersionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeBudget, FiscalYear: fiscalYear,
		VersionLabel: fmt.Sprintf("test budget %d", runID), CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create budget scenario version: %v", err)
	}

	mapping := importer.ColumnMapping{BusinessColumn: 0, DepartmentColumn: 1, AccountColumn: 2, PeriodColumn: 3, AmountColumn: 4}
	csvContent := fmt.Sprintf(
		"事業,部門,勘定科目,期間,金額\n%s,%s,%s,%d-04,950000\n%s,%s,存在しない科目%d,%d-04,1000\n",
		businessName, departmentName, accountName, fiscalYear,
		businessName, departmentName, runID, fiscalYear,
	)

	// --- reject import against a non-actual scenario version ---
	_, err = importerSvc.Commit(ctx, importer.CommitInput{
		UploadedBy: userID, OriginalFilename: "actuals.csv", FileSizeBytes: len(csvContent),
		ScenarioVersionID: uint64(budgetVersionID), Content: []byte(csvContent), Mapping: mapping,
	})
	if !errors.Is(err, importer.ErrScenarioNotActual) {
		t.Fatalf("expected ErrScenarioNotActual for a budget version, got %v", err)
	}

	// --- first import: one resolvable row, one unresolvable row ---
	result, err := importerSvc.Commit(ctx, importer.CommitInput{
		UploadedBy: userID, OriginalFilename: "actuals.csv", FileSizeBytes: len(csvContent),
		ScenarioVersionID: uint64(actualVersionID), Content: []byte(csvContent), Mapping: mapping,
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if result.RowCount != 1 {
		t.Fatalf("expected 1 resolved row, got %d", result.RowCount)
	}
	if result.ErrorCount != 1 {
		t.Fatalf("expected 1 error row, got %d (%+v)", result.ErrorCount, result.Errors)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %s", result.Status)
	}

	report, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{BusinessID: &businessID})
	if err != nil {
		t.Fatalf("variance report: %v", err)
	}
	var actualAmount *float64
	for _, row := range report {
		if row.AccountID == uint64(accountID) && row.PeriodID == periodID {
			actualAmount = row.ActualAmount
		}
	}
	if actualAmount == nil || *actualAmount != 950000 {
		t.Fatalf("actual amount = %v, want 950000", actualAmount)
	}

	// --- re-import with a corrected amount: should UPSERT, not duplicate ---
	csvContent2 := fmt.Sprintf("事業,部門,勘定科目,期間,金額\n%s,%s,%s,%d-04,960000\n", businessName, departmentName, accountName, fiscalYear)
	result2, err := importerSvc.Commit(ctx, importer.CommitInput{
		UploadedBy: userID, OriginalFilename: "actuals2.csv", FileSizeBytes: len(csvContent2),
		ScenarioVersionID: uint64(actualVersionID), Content: []byte(csvContent2), Mapping: mapping,
	})
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}
	if result2.RowCount != 1 || result2.ErrorCount != 0 {
		t.Fatalf("unexpected second import result: %+v", result2)
	}

	report2, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{BusinessID: &businessID})
	if err != nil {
		t.Fatalf("variance report 2: %v", err)
	}
	var updatedAmount *float64
	rowCountForKey := 0
	for _, row := range report2 {
		if row.AccountID == uint64(accountID) && row.PeriodID == periodID {
			updatedAmount = row.ActualAmount
			rowCountForKey++
		}
	}
	if rowCountForKey != 1 {
		t.Fatalf("expected exactly one variance report row for this dimension combination, got %d (re-import should UPSERT, not duplicate)", rowCountForKey)
	}
	if updatedAmount == nil || *updatedAmount != 960000 {
		t.Fatalf("actual amount after re-import = %v, want 960000", updatedAmount)
	}

	batches, err := importerSvc.ListBatches(ctx, uint64(actualVersionID))
	if err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected 2 import batches recorded, got %d", len(batches))
	}

	// --- Phase 4: a committed batch must be audited, with row/error counts ---
	auditSvc := audit.NewService(queries)
	logs, err := auditSvc.List(ctx, audit.ListFilter{EntityType: audit.EntityImportBatch, Limit: 100})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	var found bool
	for _, l := range logs {
		if !l.EntityID.Valid || uint64(l.EntityID.Int64) != uint64(result.ImportBatchID) {
			continue
		}
		found = true
		var detail struct {
			RowCount   int `json:"row_count"`
			ErrorCount int `json:"error_count"`
		}
		if err := json.Unmarshal(l.Detail, &detail); err != nil {
			t.Fatalf("unmarshal audit detail: %v", err)
		}
		if detail.RowCount != 1 || detail.ErrorCount != 1 {
			t.Fatalf("audit detail row/error counts = %+v, want row_count=1 error_count=1", detail)
		}
	}
	if !found {
		t.Fatalf("expected an audit_log row for import_batch %d, found none in %+v", result.ImportBatchID, logs)
	}
}
