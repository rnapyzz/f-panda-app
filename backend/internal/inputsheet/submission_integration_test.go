package inputsheet_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/inputsheet"
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

// snapshotJSON builds a minimal Univer IWorkbookData-shaped JSON snapshot
// for a single sheet named "Sheet1" with the given cell text/number grid
// (row-major, starting at A1). Numbers are written as JSON numbers so the
// backend's cellToFloat path (not cellToText) picks them up, matching what
// Univer itself would persist for a numeric cell.
func snapshotJSON(grid [][]string, numericCells map[[2]int]float64) string {
	cellData := `{`
	for r, row := range grid {
		if r > 0 {
			cellData += `,`
		}
		cellData += fmt.Sprintf(`"%d":{`, r)
		for c := range row {
			if c > 0 {
				cellData += `,`
			}
			if amt, ok := numericCells[[2]int{r, c}]; ok {
				cellData += fmt.Sprintf(`"%d":{"v":%v}`, c, amt)
			} else {
				cellData += fmt.Sprintf(`"%d":{"v":%q}`, c, grid[r][c])
			}
		}
		cellData += `}`
	}
	cellData += `}`
	return fmt.Sprintf(`{"sheets":{"sheet01":{"name":"Sheet1","cellData":%s}},"sheetOrder":["sheet01"]}`, cellData)
}

// TestSubmitBuildsFactAmountsAndDetectsShapeChanges is the Phase 2
// verification fixture from docs/plan.md: a field user's sheet (rows =
// account labels, columns = period labels, business/department fixed) is
// bound and submitted, the resulting fact_amount rows are checked against
// values computed directly from the grid, a value-only resubmission is
// confirmed to UPSERT correctly, and a resubmission after a label changes
// (simulating the user reorganizing their sheet) is confirmed to be
// rejected by the shape check rather than silently writing wrong data.
func TestSubmitBuildsFactAmountsAndDetectsShapeChanges(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	scenarios := scenario.NewService(sqlDB, queries)
	facts := fact.NewService(queries, scenarios)
	sheets := inputsheet.NewService(sqlDB, queries)

	runID := time.Now().UnixNano()
	const fiscalYear = 9002 // sentinel FY unlikely to collide with real data

	adminID, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email: fmt.Sprintf("inputsheet-test-%d@example.com", runID), Name: "Test Admin",
		Role: db.AppUserRoleOfficeAdmin, PasswordHash: "unused",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uint64(adminID)

	businessIDRaw, err := queries.CreateBusiness(ctx, db.CreateBusinessParams{Code: fmt.Sprintf("ISB-%d", runID), Name: "Test Biz"})
	if err != nil {
		t.Fatalf("create business: %v", err)
	}
	departmentIDRaw, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{Code: fmt.Sprintf("ISD-%d", runID), Name: "Test Dept"})
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	businessID, departmentID := uint64(businessIDRaw), uint64(departmentIDRaw)
	account1ID, err := queries.CreateAccount(ctx, db.CreateAccountParams{Code: fmt.Sprintf("ISA1-%d", runID), Name: "人件費", AccountType: db.DimAccountAccountTypeCost})
	if err != nil {
		t.Fatalf("create account1: %v", err)
	}
	account2ID, err := queries.CreateAccount(ctx, db.CreateAccountParams{Code: fmt.Sprintf("ISA2-%d", runID), Name: "地代家賃", AccountType: db.DimAccountAccountTypeCost})
	if err != nil {
		t.Fatalf("create account2: %v", err)
	}

	var periodIDs []uint64
	for i := 1; i <= 2; i++ {
		if err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
			FiscalYear: fiscalYear, FiscalMonth: int8(i), CalendarYear: fiscalYear, CalendarMonth: int8(i),
			StartDate: time.Date(fiscalYear, time.Month(i), 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(fiscalYear, time.Month(i), 28, 0, 0, 0, 0, time.UTC),
			Label:     fmt.Sprintf("FY%d-IS-%02d", fiscalYear, i),
		}); err != nil {
			t.Fatalf("upsert period: %v", err)
		}
		rows, err := queries.ListPeriods(ctx)
		if err != nil {
			t.Fatalf("list periods: %v", err)
		}
		for _, p := range rows {
			if p.FiscalYear == fiscalYear && p.FiscalMonth == int8(i) {
				periodIDs = append(periodIDs, p.ID)
			}
		}
	}
	period1ID, period2ID := periodIDs[0], periodIDs[1]

	budgetVersionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeBudget, FiscalYear: fiscalYear,
		VersionLabel: fmt.Sprintf("test budget %d", runID), CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create scenario version: %v", err)
	}

	// Grid: A1 blank, B1="4月", C1="5月", A2="人件費", B2=1000000, C2=1050000,
	// A3="地代家賃", B3=500000, C3=500000. Row axis = account, col axis = period.
	grid := [][]string{
		{"", "4月", "5月"},
		{"人件費", "", ""},
		{"地代家賃", "", ""},
	}
	numeric := map[[2]int]float64{{1, 1}: 1000000, {1, 2}: 1050000, {2, 1}: 500000, {2, 2}: 500000}

	sheetID, err := sheets.CreateSheet(ctx, userID, "Test Sheet", []byte(snapshotJSON(grid, numeric)))
	if err != nil {
		t.Fatalf("create sheet: %v", err)
	}

	bindingID, err := sheets.CreateBinding(ctx, inputsheet.CreateBindingInput{
		InputSheetID: uint64(sheetID), Name: "test binding", RangeSheetName: "Sheet1",
		StartRow: 0, EndRow: 2, StartCol: 0, EndCol: 2, HeaderRows: 1, HeaderCols: 1,
		RowAxisDimension: "account", ColAxisDimension: "period",
		FixedDimensions: inputsheet.FixedDimensions{BusinessID: &businessID, DepartmentID: &departmentID},
		AxisLabels: []inputsheet.AxisLabelInput{
			{Axis: "row", AxisIndex: 0, RawLabelText: "人件費", ResolvedDimensionType: "account", ResolvedDimensionID: uint64(account1ID)},
			{Axis: "row", AxisIndex: 1, RawLabelText: "地代家賃", ResolvedDimensionType: "account", ResolvedDimensionID: uint64(account2ID)},
			{Axis: "col", AxisIndex: 0, RawLabelText: "4月", ResolvedDimensionType: "period", ResolvedDimensionID: period1ID},
			{Axis: "col", AxisIndex: 1, RawLabelText: "5月", ResolvedDimensionType: "period", ResolvedDimensionID: period2ID},
		},
		CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create binding: %v", err)
	}

	// --- first submission: values should match the grid exactly ---
	result, err := sheets.Submit(ctx, inputsheet.SubmitInput{
		BindingID: uint64(bindingID), ScenarioVersionID: uint64(budgetVersionID), SubmittedBy: userID,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.ValidationStatus != "ok" {
		t.Fatalf("expected validation_status=ok, got %s (issues: %+v)", result.ValidationStatus, result.Issues)
	}
	if result.FactRowsWritten != 4 {
		t.Fatalf("expected 4 fact rows written, got %d", result.FactRowsWritten)
	}

	report, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{BusinessID: &businessID})
	if err != nil {
		t.Fatalf("variance report: %v", err)
	}
	byKey := map[[2]uint64]float64{}
	for _, row := range report {
		if row.BudgetAmount != nil {
			byKey[[2]uint64{row.AccountID, row.PeriodID}] = *row.BudgetAmount
		}
	}
	want := map[[2]uint64]float64{
		{uint64(account1ID), period1ID}: 1000000,
		{uint64(account1ID), period2ID}: 1050000,
		{uint64(account2ID), period1ID}: 500000,
		{uint64(account2ID), period2ID}: 500000,
	}
	for k, wantAmt := range want {
		if got := byKey[k]; got != wantAmt {
			t.Fatalf("fact amount for %v = %v, want %v", k, got, wantAmt)
		}
	}

	// --- resubmit with only a value changed: should UPSERT cleanly ---
	grid2 := grid
	numeric2 := map[[2]int]float64{{1, 1}: 1200000, {1, 2}: 1050000, {2, 1}: 500000, {2, 2}: 500000}
	if err := sheets.UpdateSheetSnapshot(ctx, uint64(sheetID), userID, []byte(snapshotJSON(grid2, numeric2))); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	result2, err := sheets.Submit(ctx, inputsheet.SubmitInput{
		BindingID: uint64(bindingID), ScenarioVersionID: uint64(budgetVersionID), SubmittedBy: userID,
	})
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if result2.ValidationStatus != "ok" {
		t.Fatalf("expected validation_status=ok on resubmit, got %s", result2.ValidationStatus)
	}
	report2, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{BusinessID: &businessID})
	if err != nil {
		t.Fatalf("variance report 2: %v", err)
	}
	var updatedAmount *float64
	for _, row := range report2 {
		if row.AccountID == uint64(account1ID) && row.PeriodID == period1ID {
			updatedAmount = row.BudgetAmount
		}
	}
	if updatedAmount == nil || *updatedAmount != 1200000 {
		t.Fatalf("expected updated amount 1200000, got %v", updatedAmount)
	}

	// --- resubmit after a bound label changed: must be rejected, not silently written ---
	grid3 := [][]string{
		{"", "4月", "5月"},
		{"人件費(改)", "", ""}, // row label renamed — binding's axis label no longer matches
		{"地代家賃", "", ""},
	}
	numeric3 := map[[2]int]float64{{1, 1}: 9999999, {1, 2}: 9999999, {2, 1}: 500000, {2, 2}: 500000}
	if err := sheets.UpdateSheetSnapshot(ctx, uint64(sheetID), userID, []byte(snapshotJSON(grid3, numeric3))); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	result3, err := sheets.Submit(ctx, inputsheet.SubmitInput{
		BindingID: uint64(bindingID), ScenarioVersionID: uint64(budgetVersionID), SubmittedBy: userID,
	})
	if err != nil {
		t.Fatalf("submit with changed label: %v", err)
	}
	if result3.ValidationStatus != "error" {
		t.Fatalf("expected validation_status=error after label change, got %s", result3.ValidationStatus)
	}
	if len(result3.Issues) != 1 || result3.Issues[0].Expected != "人件費" || result3.Issues[0].Actual != "人件費(改)" {
		t.Fatalf("expected exactly one issue reporting the renamed label, got %+v", result3.Issues)
	}
	if result3.FactRowsWritten != 0 {
		t.Fatalf("expected no fact rows written when the shape check fails, got %d", result3.FactRowsWritten)
	}

	// the bogus 9999999 values must NOT have overwritten the last-good amount
	report3, err := facts.VarianceReport(ctx, fiscalYear, fact.Filter{BusinessID: &businessID})
	if err != nil {
		t.Fatalf("variance report 3: %v", err)
	}
	for _, row := range report3 {
		if row.AccountID == uint64(account1ID) && row.PeriodID == period1ID {
			if row.BudgetAmount == nil || *row.BudgetAmount != 1200000 {
				t.Fatalf("fact amount was corrupted by the rejected submission: got %v, want 1200000 preserved", row.BudgetAmount)
			}
		}
	}
}
