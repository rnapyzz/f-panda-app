package fact

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

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
