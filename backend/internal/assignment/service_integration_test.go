package assignment_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/assignment"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
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

// snapshotJSON mirrors inputsheet_test's helper of the same name (kept
// package-local rather than exported, matching this repo's convention of
// small self-contained integration test fixtures per package).
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

// TestSubmissionStatusMatchesAssignmentsAndScopes is the docs/plan.md Phase
// 4 completion criterion verbatim: given 5 user_assignment fixtures, 3 of
// which have a corresponding (successful) submission against the target
// scenario_version, SubmissionStatus must report exactly those 3 as
// submitted and the other 2 as not submitted.
func TestSubmissionStatusMatchesAssignmentsAndScopes(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	scenarios := scenario.NewService(sqlDB, queries)
	sheets := inputsheet.NewService(sqlDB, queries)
	assignments := assignment.NewService(queries)

	runID := time.Now().UnixNano()
	const fiscalYear = 9004

	userIDRaw, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email: fmt.Sprintf("assign-test-%d@example.com", runID), Name: "Assign Test User",
		Role: db.AppUserRoleFieldUser, PasswordHash: "unused",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uint64(userIDRaw)

	accountIDRaw, err := queries.CreateAccount(ctx, db.CreateAccountParams{Code: fmt.Sprintf("ASA-%d", runID), Name: "割当テスト科目", AccountType: db.DimAccountAccountTypeCost})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	accountID := uint64(accountIDRaw)

	if err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
		FiscalYear: fiscalYear, FiscalMonth: 1, CalendarYear: fiscalYear, CalendarMonth: 1,
		StartDate: time.Date(fiscalYear, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(fiscalYear, 1, 31, 0, 0, 0, 0, time.UTC),
		Label:     fmt.Sprintf("FY%d-AS-01", fiscalYear),
	}); err != nil {
		t.Fatalf("upsert period: %v", err)
	}
	periods, err := queries.ListPeriods(ctx)
	if err != nil {
		t.Fatalf("list periods: %v", err)
	}
	var periodID uint64
	for _, p := range periods {
		if p.FiscalYear == fiscalYear && p.FiscalMonth == 1 {
			periodID = p.ID
		}
	}

	versionIDRaw, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeBudget, FiscalYear: fiscalYear,
		VersionLabel: fmt.Sprintf("assign test %d", runID), CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create scenario version: %v", err)
	}
	scenarioVersionID := uint64(versionIDRaw)

	// 5 business×department scopes, each with an active assignment for the
	// same user (who submitted vs. didn't isn't about who — it's about
	// which scope has a submission).
	type scope struct {
		businessID, departmentID uint64
	}
	scopes := make([]scope, 5)
	for i := 0; i < 5; i++ {
		bIDRaw, err := queries.CreateBusiness(ctx, db.CreateBusinessParams{Code: fmt.Sprintf("ASB-%d-%d", runID, i), Name: fmt.Sprintf("Assign Biz %d", i)})
		if err != nil {
			t.Fatalf("create business %d: %v", i, err)
		}
		dIDRaw, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{Code: fmt.Sprintf("ASD-%d-%d", runID, i), Name: fmt.Sprintf("Assign Dept %d", i)})
		if err != nil {
			t.Fatalf("create department %d: %v", i, err)
		}
		scopes[i] = scope{businessID: uint64(bIDRaw), departmentID: uint64(dIDRaw)}
		if _, err := assignments.Create(ctx, userID, scopes[i].businessID, scopes[i].departmentID); err != nil {
			t.Fatalf("create assignment %d: %v", i, err)
		}
	}

	// Submit for scopes 0, 1, 2 only.
	for _, sc := range scopes[:3] {
		grid := [][]string{
			{"", "4月"},
			{"割当テスト科目", ""},
		}
		numeric := map[[2]int]float64{{1, 1}: 100}
		sheetID, err := sheets.CreateSheet(ctx, userID, fmt.Sprintf("Assign Sheet %d-%d", sc.businessID, sc.departmentID), []byte(snapshotJSON(grid, numeric)))
		if err != nil {
			t.Fatalf("create sheet: %v", err)
		}
		businessID, departmentID := sc.businessID, sc.departmentID
		bindingID, err := sheets.CreateBinding(ctx, inputsheet.CreateBindingInput{
			InputSheetID: uint64(sheetID), Name: "assign binding", RangeSheetName: "Sheet1",
			StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, HeaderRows: 1, HeaderCols: 1,
			RowAxisDimension: "account", ColAxisDimension: "period",
			FixedDimensions: inputsheet.FixedDimensions{BusinessID: &businessID, DepartmentID: &departmentID},
			AxisLabels: []inputsheet.AxisLabelInput{
				{Axis: "row", AxisIndex: 0, RawLabelText: "割当テスト科目", ResolvedDimensionType: "account", ResolvedDimensionID: accountID},
				{Axis: "col", AxisIndex: 0, RawLabelText: "4月", ResolvedDimensionType: "period", ResolvedDimensionID: periodID},
			},
			CreatedBy: userID,
		})
		if err != nil {
			t.Fatalf("create binding: %v", err)
		}
		if _, err := sheets.Submit(ctx, inputsheet.SubmitInput{
			BindingID: uint64(bindingID), ScenarioVersionID: scenarioVersionID, SubmittedBy: userID,
		}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	statuses, err := assignments.SubmissionStatus(ctx, scenarioVersionID)
	if err != nil {
		t.Fatalf("submission status: %v", err)
	}

	byScope := map[scope]bool{}
	for _, st := range statuses {
		byScope[scope{businessID: st.BusinessID, departmentID: st.DepartmentID}] = st.Submitted
	}

	submittedCount, notSubmittedCount := 0, 0
	for i, sc := range scopes {
		got, ok := byScope[sc]
		if !ok {
			t.Fatalf("scope %d (business=%d, department=%d) missing from SubmissionStatus result", i, sc.businessID, sc.departmentID)
		}
		want := i < 3
		if got != want {
			t.Fatalf("scope %d submitted = %v, want %v", i, got, want)
		}
		if got {
			submittedCount++
		} else {
			notSubmittedCount++
		}
	}
	if submittedCount != 3 || notSubmittedCount != 2 {
		t.Fatalf("expected 3 submitted / 2 not submitted, got %d / %d", submittedCount, notSubmittedCount)
	}
}
