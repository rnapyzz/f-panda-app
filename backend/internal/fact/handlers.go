package fact

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/audit"
	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

func (s *Service) UpsertEntryHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ScenarioVersionID uint64  `json:"scenario_version_id"`
		BusinessID        uint64  `json:"business_id"`
		DepartmentID      uint64  `json:"department_id"`
		AccountID         uint64  `json:"account_id"`
		PeriodID          uint64  `json:"period_id"`
		Amount            float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ScenarioVersionID == 0 || req.BusinessID == 0 || req.DepartmentID == 0 || req.AccountID == 0 || req.PeriodID == 0 {
		http.Error(w, "scenario_version_id, business_id, department_id, account_id and period_id are all required", http.StatusBadRequest)
		return
	}

	err := s.UpsertEntry(r.Context(), UpsertEntryInput{
		ScenarioVersionID: req.ScenarioVersionID,
		BusinessID:        req.BusinessID,
		DepartmentID:      req.DepartmentID,
		AccountID:         req.AccountID,
		PeriodID:          req.PeriodID,
		Amount:            req.Amount,
		CreatedBy:         user.ID,
	})
	if err != nil {
		slog.Error("upsert fact entry failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// fact_amount has no single-column id here (it's keyed by the
	// scenario_version/business/department/account/period tuple), so the
	// audit row carries that tuple in Detail instead of EntityID.
	if err := audit.Record(r.Context(), s.Queries, audit.Params{
		UserID: &user.ID, Action: audit.ActionUpdate, EntityType: audit.EntityFactAmount,
		Detail: map[string]any{
			"scenario_version_id": req.ScenarioVersionID, "business_id": req.BusinessID,
			"department_id": req.DepartmentID, "account_id": req.AccountID, "period_id": req.PeriodID,
			"amount": req.Amount,
		},
		IPAddress: httpx.ClientIP(r),
	}); err != nil {
		slog.Error("audit log write failed", "error", err, "action", audit.ActionUpdate, "entity_type", audit.EntityFactAmount)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) VarianceReportHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	fiscalYear, err := strconv.Atoi(q.Get("fiscal_year"))
	if err != nil {
		http.Error(w, "fiscal_year query parameter is required", http.StatusBadRequest)
		return
	}

	filter := Filter{}
	if v := q.Get("business_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid business_id", http.StatusBadRequest)
			return
		}
		filter.BusinessID = &id
	}
	if v := q.Get("department_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid department_id", http.StatusBadRequest)
			return
		}
		filter.DepartmentID = &id
	}
	if v := q.Get("account_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "invalid account_id", http.StatusBadRequest)
			return
		}
		filter.AccountID = &id
	}

	rows, err := s.VarianceReport(r.Context(), int16(fiscalYear), filter)
	if err != nil {
		slog.Error("variance report failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}
