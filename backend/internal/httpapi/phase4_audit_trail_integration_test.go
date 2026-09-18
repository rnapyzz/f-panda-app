package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/assignment"
	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/dimension"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpapi"
	"github.com/rnapyzz/f-panda-app/backend/internal/importer"
	"github.com/rnapyzz/f-panda-app/backend/internal/inputsheet"
	"github.com/rnapyzz/f-panda-app/backend/internal/period"
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

// phase4Client wraps an httptest server + cookie-jar http.Client with the
// double-submit CSRF token captured at login, so every mutating call below
// reads like a real SPA request.
type phase4Client struct {
	t         *testing.T
	baseURL   string
	client    *http.Client
	csrfToken string
}

func (c *phase4Client) do(method, path string, body any) *http.Response {
	c.t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		c.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", want, resp.StatusCode, body)
	}
}

// TestPhase1Through3ActionsAreAllAuditTraceable is the docs/plan.md Phase 4
// completion criterion: every mutating action introduced across Phase 1-3
// (dimension CRUD, scenario version creation, fact entry, sheet/binding
// creation, submission, actuals import) plus login/logout must leave a
// traceable audit_log row. This drives the whole stack through real HTTP
// requests (not direct service calls) because Tier 2 audit hooks live in
// the handler layer, not the service layer.
func TestPhase1Through3ActionsAreAllAuditTraceable(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	queries := db.New(sqlDB)

	authSvc := auth.NewService(queries, false)
	dimensionSvc := dimension.NewService(queries)
	periodSvc := period.NewService(queries)
	scenarioSvc := scenario.NewService(sqlDB, queries)
	factSvc := fact.NewService(queries, scenarioSvc)
	inputSheetSvc := inputsheet.NewService(sqlDB, queries)
	importerSvc := importer.NewService(sqlDB, queries)
	assignmentSvc := assignment.NewService(queries)
	auditSvc := audit.NewService(queries)

	router := httpapi.NewRouter(httpapi.Deps{
		Auth: authSvc, Dimensions: dimensionSvc, Periods: periodSvc, Scenarios: scenarioSvc,
		Facts: factSvc, InputSheets: inputSheetSvc, Importer: importerSvc,
		Assignments: assignmentSvc, Audit: auditSvc,
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	runID := time.Now().UnixNano()
	const fiscalYear = 9005

	passwordHash, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	email := fmt.Sprintf("phase4-audit-%d@example.com", runID)
	adminIDRaw, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email: email, Name: "Phase4 Audit Admin", Role: db.AppUserRoleOfficeAdmin, PasswordHash: passwordHash,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	adminID := uint64(adminIDRaw)

	if err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
		FiscalYear: fiscalYear, FiscalMonth: 1, CalendarYear: fiscalYear, CalendarMonth: 1,
		StartDate: time.Date(fiscalYear, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(fiscalYear, 1, 31, 0, 0, 0, 0, time.UTC),
		Label:     fmt.Sprintf("FY%d-P4-01", fiscalYear),
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

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &phase4Client{t: t, baseURL: server.URL, client: &http.Client{Jar: jar}}

	// --- login (captures the non-HttpOnly CSRF cookie for later writes) ---
	loginResp := c.do(http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": "test-password"})
	requireStatus(t, loginResp, http.StatusOK)
	for _, ck := range loginResp.Cookies() {
		if !ck.HttpOnly {
			c.csrfToken = ck.Value
		}
	}
	if c.csrfToken == "" {
		t.Fatal("did not find a non-HttpOnly (CSRF) cookie on the login response")
	}

	// --- dimension master CRUD ---
	bizResp := c.do(http.MethodPost, "/api/businesses", map[string]string{"code": fmt.Sprintf("P4B-%d", runID), "name": "Phase4 Biz"})
	requireStatus(t, bizResp, http.StatusCreated)
	businessID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, bizResp).ID
	requireStatus(t, c.do(http.MethodPut, fmt.Sprintf("/api/businesses/%d", businessID),
		map[string]any{"code": fmt.Sprintf("P4B-%d", runID), "name": "Phase4 Biz Updated", "is_active": true}), http.StatusOK)

	deptResp := c.do(http.MethodPost, "/api/departments", map[string]string{"code": fmt.Sprintf("P4D-%d", runID), "name": "Phase4 Dept"})
	requireStatus(t, deptResp, http.StatusCreated)
	departmentID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, deptResp).ID
	requireStatus(t, c.do(http.MethodPut, fmt.Sprintf("/api/departments/%d", departmentID),
		map[string]any{"code": fmt.Sprintf("P4D-%d", runID), "name": "Phase4 Dept Updated", "is_active": true}), http.StatusOK)

	acctResp := c.do(http.MethodPost, "/api/accounts", map[string]string{"code": fmt.Sprintf("P4A-%d", runID), "name": "Phase4 Account", "account_type": "cost"})
	requireStatus(t, acctResp, http.StatusCreated)
	accountID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, acctResp).ID
	requireStatus(t, c.do(http.MethodPut, fmt.Sprintf("/api/accounts/%d", accountID),
		map[string]any{"code": fmt.Sprintf("P4A-%d", runID), "name": "Phase4 Account Updated", "account_type": "cost", "is_active": true}), http.StatusOK)

	// --- scenario version creation ---
	budgetResp := c.do(http.MethodPost, "/api/scenario-versions", map[string]any{
		"scenario_type": "budget", "fiscal_year": fiscalYear, "version_label": fmt.Sprintf("phase4 budget %d", runID),
	})
	requireStatus(t, budgetResp, http.StatusCreated)
	budgetVersionID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, budgetResp).ID

	actualResp := c.do(http.MethodPost, "/api/scenario-versions", map[string]any{
		"scenario_type": "actual", "fiscal_year": fiscalYear, "version_label": fmt.Sprintf("phase4 actual %d", runID),
	})
	requireStatus(t, actualResp, http.StatusCreated)
	actualVersionID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, actualResp).ID

	// --- sheet + binding + submission (Phase 2 input layer) ---
	grid := [][]string{{"", "4月"}, {"Phase4 Account Updated", ""}}
	numeric := map[[2]int]float64{{1, 1}: 500}
	sheetResp := c.do(http.MethodPost, "/api/sheets", map[string]any{"name": "Phase4 Sheet", "snapshot": json.RawMessage(snapshotJSON(grid, numeric))})
	requireStatus(t, sheetResp, http.StatusCreated)
	sheetID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, sheetResp).ID
	requireStatus(t, c.do(http.MethodPut, fmt.Sprintf("/api/sheets/%d", sheetID),
		map[string]any{"snapshot": json.RawMessage(snapshotJSON(grid, numeric))}), http.StatusNoContent)

	bindingResp := c.do(http.MethodPost, fmt.Sprintf("/api/sheets/%d/bindings", sheetID), map[string]any{
		"name": "phase4 binding", "range_sheet_name": "Sheet1",
		"start_row": 0, "end_row": 1, "start_col": 0, "end_col": 1, "header_rows": 1, "header_cols": 1,
		"row_axis_dimension": "account", "col_axis_dimension": "period",
		"fixed_dimensions": map[string]uint64{"business_id": businessID, "department_id": departmentID},
		"axis_labels": []map[string]any{
			{"axis": "row", "axis_index": 0, "raw_label_text": "Phase4 Account Updated", "resolved_dimension_type": "account", "resolved_dimension_id": accountID},
			{"axis": "col", "axis_index": 0, "raw_label_text": "4月", "resolved_dimension_type": "period", "resolved_dimension_id": periodID},
		},
	})
	requireStatus(t, bindingResp, http.StatusCreated)
	bindingID := decodeJSON[struct {
		ID uint64 `json:"id"`
	}](t, bindingResp).ID

	submitResp := c.do(http.MethodPost, "/api/submissions", map[string]any{"binding_id": bindingID, "scenario_version_id": budgetVersionID})
	requireStatus(t, submitResp, http.StatusOK)
	submitResult := decodeJSON[struct {
		ValidationStatus string `json:"validation_status"`
	}](t, submitResp)
	if submitResult.ValidationStatus != "ok" {
		t.Fatalf("expected submission validation_status=ok, got %s", submitResult.ValidationStatus)
	}

	// --- actuals import (Phase 3) ---
	csvContent := fmt.Sprintf("事業,部門,勘定科目,期間,金額\nP4B-%d,P4D-%d,P4A-%d,%d-01,777\n", runID, runID, runID, fiscalYear)
	var multipartBody bytes.Buffer
	mw := multipart.NewWriter(&multipartBody)
	fw, err := mw.CreateFormFile("file", "actuals.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(csvContent)); err != nil {
		t.Fatalf("write csv content: %v", err)
	}
	mapping, _ := json.Marshal(importer.ColumnMapping{BusinessColumn: 0, DepartmentColumn: 1, AccountColumn: 2, PeriodColumn: 3, AmountColumn: 4})
	_ = mw.WriteField("mapping", string(mapping))
	_ = mw.WriteField("scenario_version_id", fmt.Sprintf("%d", actualVersionID))
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	importReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/import-batches", &multipartBody)
	if err != nil {
		t.Fatalf("build import request: %v", err)
	}
	importReq.Header.Set("Content-Type", mw.FormDataContentType())
	importReq.Header.Set("X-CSRF-Token", c.csrfToken)
	importResp, err := c.client.Do(importReq)
	if err != nil {
		t.Fatalf("import request: %v", err)
	}
	requireStatus(t, importResp, http.StatusOK)

	// --- logout ---
	requireStatus(t, c.do(http.MethodPost, "/api/auth/logout", nil), http.StatusNoContent)

	// --- assert every (action, entity_type) pair introduced by Phase 1-3 has
	// at least one traceable audit_log row for this admin ---
	logs, err := auditSvc.List(ctx, audit.ListFilter{UserID: &adminID, Limit: 200})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	seen := map[string]bool{}
	for _, l := range logs {
		seen[l.Action+"|"+l.EntityType] = true
	}

	wantPairs := []string{
		audit.ActionLogin + "|" + audit.EntityUser,
		audit.ActionLogout + "|" + audit.EntityUser,
		audit.ActionCreate + "|" + audit.EntityBusiness,
		audit.ActionUpdate + "|" + audit.EntityBusiness,
		audit.ActionCreate + "|" + audit.EntityDepartment,
		audit.ActionUpdate + "|" + audit.EntityDepartment,
		audit.ActionCreate + "|" + audit.EntityAccount,
		audit.ActionUpdate + "|" + audit.EntityAccount,
		audit.ActionCreate + "|" + audit.EntityScenarioVersion,
		audit.ActionCreate + "|" + audit.EntityInputSheet,
		audit.ActionUpdate + "|" + audit.EntityInputSheet,
		audit.ActionCreate + "|" + audit.EntityInputBinding,
		audit.ActionSubmit + "|" + audit.EntitySubmission,
		audit.ActionImport + "|" + audit.EntityImportBatch,
	}
	var missing []string
	for _, want := range wantPairs {
		if !seen[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing audit_log entries for: %v (all logs for this user: %+v)", missing, logs)
	}
}
