package inputsheet_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/inputsheet"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
)

// TestSubmitWritesSubmissionScopeAndAuditLog is the Phase 4 verification
// fixture: a successful submission must record a submission_scope row for
// every (business, department) it wrote fact_amount for, and — regardless
// of whether the submission passed or failed its shape check — a
// corresponding audit_log row. submission_scope and audit_log are
// deliberately asymmetric: a failed (validation_status=error) submission
// still gets audited (事務局 should be able to trace even rejected attempts)
// but never gets a submission_scope row (nothing was actually written to
// fact_amount for it to describe).
func TestSubmitWritesSubmissionScopeAndAuditLog(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)
	scenarios := scenario.NewService(sqlDB, queries)
	sheets := inputsheet.NewService(sqlDB, queries)
	auditSvc := audit.NewService(queries)

	runID := time.Now().UnixNano()
	const fiscalYear = 9003

	userIDRaw, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email: fmt.Sprintf("scope-test-%d@example.com", runID), Name: "Test User",
		Role: db.AppUserRoleFieldUser, PasswordHash: "unused",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	userID := uint64(userIDRaw)

	businessIDRaw, err := queries.CreateBusiness(ctx, db.CreateBusinessParams{Code: fmt.Sprintf("SCB-%d", runID), Name: "Scope Biz"})
	if err != nil {
		t.Fatalf("create business: %v", err)
	}
	departmentIDRaw, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{Code: fmt.Sprintf("SCD-%d", runID), Name: "Scope Dept"})
	if err != nil {
		t.Fatalf("create department: %v", err)
	}
	businessID, departmentID := uint64(businessIDRaw), uint64(departmentIDRaw)

	accountIDRaw, err := queries.CreateAccount(ctx, db.CreateAccountParams{Code: fmt.Sprintf("SCA-%d", runID), Name: "テスト科目", AccountType: db.DimAccountAccountTypeCost})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	accountID := uint64(accountIDRaw)

	if err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
		FiscalYear: fiscalYear, FiscalMonth: 1, CalendarYear: fiscalYear, CalendarMonth: 1,
		StartDate: time.Date(fiscalYear, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(fiscalYear, 1, 31, 0, 0, 0, 0, time.UTC),
		Label:     fmt.Sprintf("FY%d-SC-01", fiscalYear),
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

	versionID, err := scenarios.Create(ctx, scenario.CreateInput{
		ScenarioType: db.ScenarioVersionScenarioTypeBudget, FiscalYear: fiscalYear,
		VersionLabel: fmt.Sprintf("scope test %d", runID), CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create scenario version: %v", err)
	}
	scenarioVersionID := uint64(versionID)

	grid := [][]string{
		{"", "4月"},
		{"テスト科目", ""},
	}
	numeric := map[[2]int]float64{{1, 1}: 100}

	sheetID, err := sheets.CreateSheet(ctx, userID, "Scope Sheet", []byte(snapshotJSON(grid, numeric)))
	if err != nil {
		t.Fatalf("create sheet: %v", err)
	}

	bindingID, err := sheets.CreateBinding(ctx, inputsheet.CreateBindingInput{
		InputSheetID: uint64(sheetID), Name: "scope binding", RangeSheetName: "Sheet1",
		StartRow: 0, EndRow: 1, StartCol: 0, EndCol: 1, HeaderRows: 1, HeaderCols: 1,
		RowAxisDimension: "account", ColAxisDimension: "period",
		FixedDimensions: inputsheet.FixedDimensions{BusinessID: &businessID, DepartmentID: &departmentID},
		AxisLabels: []inputsheet.AxisLabelInput{
			{Axis: "row", AxisIndex: 0, RawLabelText: "テスト科目", ResolvedDimensionType: "account", ResolvedDimensionID: accountID},
			{Axis: "col", AxisIndex: 0, RawLabelText: "4月", ResolvedDimensionType: "period", ResolvedDimensionID: periodID},
		},
		CreatedBy: userID,
	})
	if err != nil {
		t.Fatalf("create binding: %v", err)
	}

	// --- successful submission: expect submission_scope + audit_log ---
	result, err := sheets.Submit(ctx, inputsheet.SubmitInput{
		BindingID: uint64(bindingID), ScenarioVersionID: scenarioVersionID, SubmittedBy: userID,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.ValidationStatus != "ok" {
		t.Fatalf("expected validation_status=ok, got %s", result.ValidationStatus)
	}

	scopes, err := queries.ListSubmissionScopesByScenarioVersion(ctx, scenarioVersionID)
	if err != nil {
		t.Fatalf("list submission scopes: %v", err)
	}
	if len(scopes) != 1 {
		t.Fatalf("expected exactly 1 submission_scope row after a successful submit, got %d", len(scopes))
	}
	if scopes[0].BusinessID != businessID || scopes[0].DepartmentID != departmentID {
		t.Fatalf("submission_scope has wrong scope: got (business=%d, department=%d), want (%d, %d)",
			scopes[0].BusinessID, scopes[0].DepartmentID, businessID, departmentID)
	}
	if uint64(scopes[0].SubmissionID) != uint64(result.SubmissionID) {
		t.Fatalf("submission_scope.submission_id = %d, want %d", scopes[0].SubmissionID, result.SubmissionID)
	}

	logs, err := auditSvc.List(ctx, audit.ListFilter{EntityType: audit.EntitySubmission, Limit: 100})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if !hasAuditEntry(logs, uint64(result.SubmissionID)) {
		t.Fatalf("expected an audit_log row for the successful submission %d, found none in %+v", result.SubmissionID, logs)
	}

	// --- resubmit after breaking the bound label: validation error, so no
	// new submission_scope row, but the attempt itself must still be audited ---
	grid2 := [][]string{
		{"", "4月"},
		{"テスト科目(改)", ""},
	}
	if err := sheets.UpdateSheetSnapshot(ctx, uint64(sheetID), userID, []byte(snapshotJSON(grid2, numeric))); err != nil {
		t.Fatalf("update snapshot: %v", err)
	}
	result2, err := sheets.Submit(ctx, inputsheet.SubmitInput{
		BindingID: uint64(bindingID), ScenarioVersionID: scenarioVersionID, SubmittedBy: userID,
	})
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if result2.ValidationStatus != "error" {
		t.Fatalf("expected validation_status=error, got %s", result2.ValidationStatus)
	}

	scopesAfter, err := queries.ListSubmissionScopesByScenarioVersion(ctx, scenarioVersionID)
	if err != nil {
		t.Fatalf("list submission scopes after failed resubmit: %v", err)
	}
	if len(scopesAfter) != 1 {
		t.Fatalf("expected submission_scope to remain at 1 row after a failed resubmit, got %d", len(scopesAfter))
	}

	logs2, err := auditSvc.List(ctx, audit.ListFilter{EntityType: audit.EntitySubmission, Limit: 100})
	if err != nil {
		t.Fatalf("list audit logs after failed resubmit: %v", err)
	}
	if !hasAuditEntry(logs2, uint64(result2.SubmissionID)) {
		t.Fatalf("expected an audit_log row for the FAILED submission %d too, found none in %+v", result2.SubmissionID, logs2)
	}
}

func hasAuditEntry(logs []db.ListAuditLogsRow, entityID uint64) bool {
	for _, l := range logs {
		if l.EntityID.Valid && uint64(l.EntityID.Int64) == entityID {
			return true
		}
	}
	return false
}
