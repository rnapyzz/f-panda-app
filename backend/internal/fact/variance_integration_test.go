package fact_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
)

// openTestDB connects to the database named by APP_DB_DSN (set by CI against
// a real MySQL service container; see docker-compose.yml / README.md to run
// this locally). The test is skipped, not failed, when no database is
// reachable, since this is an opt-in integration test rather than a unit
// test.
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

// TestVarianceReportMatchesHandComputedExpectations is the Phase 1
// verification fixture from docs/plan.md: 3 businesses x 2 departments x 5
// accounts x 3 months of budget, with actual entered for a known subset, and
// asserts the variance report's numbers match values computed the same way
// docs/plan.md's fixture describes (hand-derivable from the entry formula).
func TestVarianceReportMatchesHandComputedExpectations(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	scenarios := scenario.NewService(sqlDB, queries)
	facts := fact.NewService(queries, scenarios)

	runID := time.Now().UnixNano()
	const fiscalYear = 9001 // sentinel FY unlikely to collide with real data

	// --- fixture: 1 admin user, 3 businesses, 2 departments, 5 accounts ---
	adminID, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        fmt.Sprintf("variance-test-%d@example.com", runID),
		Name:         "Variance Test Admin",
		Role:         db.AppUserRoleOfficeAdmin,
		PasswordHash: "unused",
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	var businessIDs []uint64
	for i := 1; i <= 3; i++ {
		id, err := queries.CreateBusiness(ctx, db.CreateBusinessParams{
			Code: fmt.Sprintf("TESTBIZ-%d-%d", runID, i),
			Name: fmt.Sprintf("Test Business %d", i),
		})
		if err != nil {
			t.Fatalf("create business: %v", err)
		}
		businessIDs = append(businessIDs, uint64(id))
	}

	var departmentIDs []uint64
	for i := 1; i <= 2; i++ {
		id, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{
			Code: fmt.Sprintf("TESTDEPT-%d-%d", runID, i),
			Name: fmt.Sprintf("Test Department %d", i),
		})
		if err != nil {
			t.Fatalf("create department: %v", err)
		}
		departmentIDs = append(departmentIDs, uint64(id))
	}

	var accountIDs []uint64
	for i := 1; i <= 5; i++ {
		id, err := queries.CreateAccount(ctx, db.CreateAccountParams{
			Code:        fmt.Sprintf("TESTACC-%d-%d", runID, i),
			Name:        fmt.Sprintf("Test Account %d", i),
			AccountType: db.DimAccountAccountTypeCost,
		})
		if err != nil {
			t.Fatalf("create account: %v", err)
		}
		accountIDs = append(accountIDs, uint64(id))
	}

	var periodIDs []uint64
	for i := 1; i <= 3; i++ {
		err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
			FiscalYear:    fiscalYear,
			FiscalMonth:   int8(i),
			CalendarYear:  fiscalYear,
			CalendarMonth: int8(i),
			StartDate:     time.Date(fiscalYear, time.Month(i), 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(fiscalYear, time.Month(i), 28, 0, 0, 0, 0, time.UTC),
			Label:         fmt.Sprintf("FY%d-TEST-%02d", fiscalYear, i),
		})
		if err != nil {
			t.Fatalf("upsert period: %v", err)
		}
		var p db.DimPeriod
		for _, row := range mustListPeriods(t, ctx, queries) {
			if row.FiscalYear == fiscalYear && row.FiscalMonth == int8(i) {
				p = row
				break
			}
		}
		periodIDs = append(periodIDs, p.ID)
	}

	// --- scenario versions: budget and actual (forecast left empty) ---
	budgetVersionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeBudget,
		FiscalYear:   fiscalYear,
		VersionLabel: fmt.Sprintf("test budget %d", runID),
		CreatedBy:    uint64(adminID),
	})
	if err != nil {
		t.Fatalf("create budget scenario version: %v", err)
	}
	actualVersionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeActual,
		FiscalYear:   fiscalYear,
		VersionLabel: fmt.Sprintf("test actual %d", runID),
		CreatedBy:    uint64(adminID),
	})
	if err != nil {
		t.Fatalf("create actual scenario version: %v", err)
	}

	// budget(b,d,a,p) = 1000*b + 100*d + 10*a + p (1-based indices)
	// actual is only entered for the first business, and equals budget+42.
	const actualDelta = 42.0
	budgetOf := func(bIdx, dIdx, aIdx, pIdx int) float64 {
		return float64(1000*bIdx + 100*dIdx + 10*aIdx + pIdx)
	}

	for bIdx, businessID := range businessIDs {
		for dIdx, departmentID := range departmentIDs {
			for aIdx, accountID := range accountIDs {
				for pIdx, periodID := range periodIDs {
					amount := budgetOf(bIdx+1, dIdx+1, aIdx+1, pIdx+1)
					// Fact rows are seeded directly via the sheet-binding
					// upsert query (with no submission_id) rather than
					// through a manual-entry API, since that API no longer
					// exists — only sheet-binding and CSV-import write
					// fact_amount now.
					if err := queries.UpsertFactAmountFromSubmission(ctx, db.UpsertFactAmountFromSubmissionParams{
						ScenarioVersionID: uint64(budgetVersionID),
						BusinessID:        businessID,
						DepartmentID:      departmentID,
						AccountID:         accountID,
						PeriodID:          periodID,
						Amount:            strconv.FormatFloat(amount, 'f', 2, 64),
						SubmissionID:      sql.NullInt64{Valid: false},
						CreatedBy:         uint64(adminID),
					}); err != nil {
						t.Fatalf("upsert budget entry: %v", err)
					}

					if bIdx == 0 { // only the first business has actuals entered
						if err := queries.UpsertFactAmountFromSubmission(ctx, db.UpsertFactAmountFromSubmissionParams{
							ScenarioVersionID: uint64(actualVersionID),
							BusinessID:        businessID,
							DepartmentID:      departmentID,
							AccountID:         accountID,
							PeriodID:          periodID,
							Amount:            strconv.FormatFloat(amount+actualDelta, 'f', 2, 64),
							SubmissionID:      sql.NullInt64{Valid: false},
							CreatedBy:         uint64(adminID),
						}); err != nil {
							t.Fatalf("upsert actual entry: %v", err)
						}
					}
				}
			}
		}
	}

	report, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{})
	if err != nil {
		t.Fatalf("variance report: %v", err)
	}

	// Restrict to rows from this test run's own businesses, since the
	// fixture fiscal year (9001) could in principle be shared by other
	// concurrent test runs.
	ownBusiness := map[uint64]bool{}
	for _, id := range businessIDs {
		ownBusiness[id] = true
	}
	var rows []fact.VarianceRow
	for _, row := range report {
		if ownBusiness[row.BusinessID] {
			rows = append(rows, row)
		}
	}

	const wantRows = 3 * 2 * 5 * 3 // businesses x departments x accounts x periods
	if len(rows) != wantRows {
		t.Fatalf("expected %d variance rows, got %d", wantRows, len(rows))
	}

	businessIndex := make(map[uint64]int, len(businessIDs))
	for i, id := range businessIDs {
		businessIndex[id] = i
	}

	gotWithActual := 0
	for _, row := range rows {
		bIdx := businessIndex[row.BusinessID]

		if row.BudgetAmount == nil {
			t.Fatalf("row %+v: expected a budget amount", row)
		}
		if row.ForecastAmount != nil {
			t.Fatalf("row %+v: expected no forecast amount (none was entered)", row)
		}

		if bIdx == 0 {
			gotWithActual++
			if row.ActualAmount == nil {
				t.Fatalf("row %+v: expected an actual amount for the first business", row)
			}
			if *row.ActualAmount != *row.BudgetAmount+actualDelta {
				t.Fatalf("row %+v: actual = %.2f, want budget(%.2f)+%.2f", row, *row.ActualAmount, *row.BudgetAmount, actualDelta)
			}
			if row.VarianceAmount == nil || *row.VarianceAmount != actualDelta {
				t.Fatalf("row %+v: variance = %v, want %.2f", row, row.VarianceAmount, actualDelta)
			}
		} else {
			if row.ActualAmount != nil {
				t.Fatalf("row %+v: expected no actual amount for businesses other than the first", row)
			}
			if row.VarianceAmount != nil {
				t.Fatalf("row %+v: expected no variance amount without an actual", row)
			}
		}
	}
	if wantWithActual := 1 * 2 * 5 * 3; gotWithActual != wantWithActual {
		t.Fatalf("expected %d rows with actuals, got %d", wantWithActual, gotWithActual)
	}

	// Spot check one specific hand-computed cell: business#2, department#1,
	// account#3, period#2 => budget = 1000*2 + 100*1 + 10*3 + 2 = 2132.
	var found bool
	for _, row := range rows {
		if businessIndex[row.BusinessID] == 1 &&
			row.DepartmentCode == fmt.Sprintf("TESTDEPT-%d-1", runID) &&
			row.AccountCode == fmt.Sprintf("TESTACC-%d-3", runID) &&
			row.FiscalMonth == 2 {
			found = true
			if row.BudgetAmount == nil || *row.BudgetAmount != 2132 {
				t.Fatalf("spot-check row %+v: budget = %v, want 2132", row, row.BudgetAmount)
			}
		}
	}
	if !found {
		t.Fatal("spot-check row (business#2, department#1, account#3, period#2) not found in report")
	}
}

func mustListPeriods(t *testing.T, ctx context.Context, q *db.Queries) []db.DimPeriod {
	t.Helper()
	rows, err := q.ListPeriods(ctx)
	if err != nil {
		t.Fatalf("list periods: %v", err)
	}
	return rows
}
