package scenario

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type versionDTO struct {
	ID           uint64  `json:"id"`
	ScenarioType string  `json:"scenario_type"`
	FiscalYear   int16   `json:"fiscal_year"`
	AsOfPeriodID *uint64 `json:"as_of_period_id,omitempty"`
	VersionLabel string  `json:"version_label"`
	Status       string  `json:"status"`
	IsCurrent    bool    `json:"is_current"`
}

func toVersionDTO(v db.ScenarioVersion) versionDTO {
	dto := versionDTO{
		ID:           v.ID,
		ScenarioType: string(v.ScenarioType),
		FiscalYear:   v.FiscalYear,
		VersionLabel: v.VersionLabel,
		Status:       string(v.Status),
		IsCurrent:    v.IsCurrent,
	}
	if v.AsOfPeriodID.Valid {
		id := uint64(v.AsOfPeriodID.Int64)
		dto.AsOfPeriodID = &id
	}
	return dto
}

var validScenarioTypes = map[string]db.ScenarioVersionScenarioType{
	"budget":   db.ScenarioVersionScenarioTypeBudget,
	"forecast": db.ScenarioVersionScenarioTypeForecast,
	"actual":   db.ScenarioVersionScenarioTypeActual,
}

func parseScenarioType(w http.ResponseWriter, s string) (db.ScenarioVersionScenarioType, bool) {
	t, ok := validScenarioTypes[s]
	if !ok {
		http.Error(w, "scenario_type must be one of budget, forecast, actual", http.StatusBadRequest)
		return "", false
	}
	return t, true
}

func (s *Service) ListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scenarioType, ok := parseScenarioType(w, q.Get("scenario_type"))
	if !ok {
		return
	}
	fiscalYear, ok := parseFiscalYear(w, q.Get("fiscal_year"))
	if !ok {
		return
	}

	rows, err := s.List(r.Context(), scenarioType, fiscalYear)
	if err != nil {
		slog.Error("list scenario versions failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]versionDTO, len(rows))
	for i, row := range rows {
		out[i] = toVersionDTO(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) CreateHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		ScenarioType string  `json:"scenario_type"`
		FiscalYear   int16   `json:"fiscal_year"`
		AsOfPeriodID *uint64 `json:"as_of_period_id"`
		VersionLabel string  `json:"version_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	scenarioType, ok := parseScenarioType(w, req.ScenarioType)
	if !ok {
		return
	}
	if req.VersionLabel == "" || req.FiscalYear == 0 {
		http.Error(w, "fiscal_year and version_label are required", http.StatusBadRequest)
		return
	}

	id, err := s.Create(r.Context(), CreateInput{
		ScenarioType: scenarioType,
		FiscalYear:   req.FiscalYear,
		AsOfPeriodID: req.AsOfPeriodID,
		VersionLabel: req.VersionLabel,
		CreatedBy:    user.ID,
	})
	if err != nil {
		if errors.Is(err, ErrForecastRequiresAsOfPeriod) || errors.Is(err, ErrAsOfPeriodOnlyForForecast) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("create scenario version failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]uint64{"id": uint64(id)})
}

func parseFiscalYear(w http.ResponseWriter, s string) (int16, bool) {
	if s == "" {
		http.Error(w, "fiscal_year query parameter is required", http.StatusBadRequest)
		return 0, false
	}
	year, err := strconv.Atoi(s)
	if err != nil || year < 2000 || year > 3000 {
		http.Error(w, "invalid fiscal_year", http.StatusBadRequest)
		return 0, false
	}
	return int16(year), true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
