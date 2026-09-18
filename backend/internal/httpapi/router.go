// Package httpapi wires the application's HTTP routes and middleware chain
// on top of the standard library's net/http.ServeMux (Go 1.22+ method+path
// patterns), deliberately without a third-party router framework.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/dimension"
	"github.com/rnapyzz/f-panda-app/backend/internal/fact"
	"github.com/rnapyzz/f-panda-app/backend/internal/importer"
	"github.com/rnapyzz/f-panda-app/backend/internal/inputsheet"
	"github.com/rnapyzz/f-panda-app/backend/internal/period"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
)

type Deps struct {
	Auth        *auth.Service
	Dimensions  *dimension.Service
	Periods     *period.Service
	Scenarios   *scenario.Service
	Facts       *fact.Service
	InputSheets *inputsheet.Service
	Importer    *importer.Service
	SPAHandler  http.Handler
}

func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)

	// The login endpoint is deliberately exempt from CSRF checking: the CSRF
	// cookie doesn't exist until a session does, so requiring it here would
	// make it impossible to ever log in. Every other mutating route goes
	// through both authed (session) and CSRF checks.
	mux.HandleFunc("POST /api/auth/login", deps.Auth.LoginHandler)
	mux.Handle("POST /api/auth/logout", deps.authedMutating(http.HandlerFunc(deps.Auth.LogoutHandler)))
	mux.Handle("GET /api/auth/me", deps.authed(http.HandlerFunc(deps.Auth.MeHandler)))

	// Dimension masters: 事務局 (office_admin) owns the fixed schema, so
	// writes are admin-only; reads are open to any authenticated user
	// (field users need the lists for the entry form).
	mux.Handle("GET /api/businesses", deps.authed(http.HandlerFunc(deps.Dimensions.ListBusinessesHandler)))
	mux.Handle("POST /api/businesses", deps.adminMutating(http.HandlerFunc(deps.Dimensions.CreateBusinessHandler)))
	mux.Handle("PUT /api/businesses/{id}", deps.adminMutating(http.HandlerFunc(deps.Dimensions.UpdateBusinessHandler)))

	mux.Handle("GET /api/departments", deps.authed(http.HandlerFunc(deps.Dimensions.ListDepartmentsHandler)))
	mux.Handle("POST /api/departments", deps.adminMutating(http.HandlerFunc(deps.Dimensions.CreateDepartmentHandler)))
	mux.Handle("PUT /api/departments/{id}", deps.adminMutating(http.HandlerFunc(deps.Dimensions.UpdateDepartmentHandler)))

	mux.Handle("GET /api/accounts", deps.authed(http.HandlerFunc(deps.Dimensions.ListAccountsHandler)))
	mux.Handle("POST /api/accounts", deps.adminMutating(http.HandlerFunc(deps.Dimensions.CreateAccountHandler)))
	mux.Handle("PUT /api/accounts/{id}", deps.adminMutating(http.HandlerFunc(deps.Dimensions.UpdateAccountHandler)))

	mux.Handle("GET /api/periods", deps.authed(http.HandlerFunc(deps.Periods.ListPeriodsHandler)))

	// Scenario versions: starting a new budget/forecast/actual cycle is an
	// office_admin action; anyone authenticated can see what versions exist
	// so they know which scenario_version_id to enter data against.
	mux.Handle("GET /api/scenario-versions", deps.authed(http.HandlerFunc(deps.Scenarios.ListHandler)))
	mux.Handle("POST /api/scenario-versions", deps.adminMutating(http.HandlerFunc(deps.Scenarios.CreateHandler)))

	// Fact entry (the Phase 1 stand-in for the spreadsheet input layer) and
	// the variance report are open to any authenticated user.
	mux.Handle("POST /api/fact-entries", deps.authedMutating(http.HandlerFunc(deps.Facts.UpsertEntryHandler)))
	mux.Handle("GET /api/variance-report", deps.authed(http.HandlerFunc(deps.Facts.VarianceReportHandler)))

	// Input sheets are a field user's own workspace (ownership is checked
	// inside each handler, not just by session — a sheet belongs to
	// whoever created it, not to office_admin generally).
	mux.Handle("GET /api/sheets", deps.authed(http.HandlerFunc(deps.InputSheets.ListSheetsHandler)))
	mux.Handle("POST /api/sheets", deps.authedMutating(http.HandlerFunc(deps.InputSheets.CreateSheetHandler)))
	mux.Handle("GET /api/sheets/{id}", deps.authed(http.HandlerFunc(deps.InputSheets.GetSheetHandler)))
	mux.Handle("PUT /api/sheets/{id}", deps.authedMutating(http.HandlerFunc(deps.InputSheets.UpdateSheetHandler)))
	mux.Handle("GET /api/sheets/{id}/bindings", deps.authed(http.HandlerFunc(deps.InputSheets.ListBindingsHandler)))
	mux.Handle("POST /api/sheets/{id}/bindings", deps.authedMutating(http.HandlerFunc(deps.InputSheets.CreateBindingHandler)))
	mux.Handle("POST /api/submissions", deps.authedMutating(http.HandlerFunc(deps.InputSheets.SubmitHandler)))

	// Actuals import (CSV/XLSX) is office_admin-only: it writes 実績 data
	// across the whole organization, not just the uploader's own scope.
	mux.Handle("POST /api/import-batches/preview", deps.adminMutating(http.HandlerFunc(importer.PreviewHandler)))
	mux.Handle("POST /api/import-batches", deps.adminMutating(http.HandlerFunc(deps.Importer.CommitHandler)))
	mux.Handle("GET /api/import-batches", deps.authed(http.HandlerFunc(deps.Importer.ListBatchesHandler)))

	if deps.SPAHandler != nil {
		mux.Handle("/", deps.SPAHandler)
	}

	return withMiddleware(mux)
}

// authed requires a valid session, no CSRF check (safe for GET/HEAD).
func (deps Deps) authed(h http.Handler) http.Handler {
	return deps.Auth.RequireAuth(h)
}

// authedMutating requires a valid session and a matching CSRF token, for
// any authenticated user's mutating requests.
func (deps Deps) authedMutating(h http.Handler) http.Handler {
	return deps.Auth.RequireAuth(auth.RequireCSRF(h))
}

// adminMutating additionally requires the office_admin role, for mutating
// requests that change the shared schema/master data.
func (deps Deps) adminMutating(h http.Handler) http.Handler {
	return deps.Auth.RequireAuth(auth.RequireCSRF(auth.RequireRole(db.AppUserRoleOfficeAdmin, h)))
}

func withMiddleware(h http.Handler) http.Handler {
	return recoverMiddleware(logMiddleware(h))
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
