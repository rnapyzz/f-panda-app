// Package period exposes the fiscal calendar (dim_period), which is
// populated out-of-band by cmd/seedperiods rather than through the HTTP API.
package period

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

type Service struct {
	Queries *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{Queries: q}
}

type periodDTO struct {
	ID            uint64 `json:"id"`
	FiscalYear    int16  `json:"fiscal_year"`
	FiscalMonth   int16  `json:"fiscal_month"`
	CalendarYear  int16  `json:"calendar_year"`
	CalendarMonth int16  `json:"calendar_month"`
	Label         string `json:"label"`
}

func toPeriodDTO(p db.DimPeriod) periodDTO {
	return periodDTO{
		ID:            p.ID,
		FiscalYear:    p.FiscalYear,
		FiscalMonth:   int16(p.FiscalMonth),
		CalendarYear:  p.CalendarYear,
		CalendarMonth: int16(p.CalendarMonth),
		Label:         p.Label,
	}
}

func (s *Service) ListPeriodsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Queries.ListPeriods(r.Context())
	if err != nil {
		slog.Error("list periods failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]periodDTO, len(rows))
	for i, row := range rows {
		out[i] = toPeriodDTO(row)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ListPeriods is a small convenience wrapper used by other services (e.g.
// fact) that need the full period list rather than an HTTP response.
func (s *Service) ListPeriods(ctx context.Context) ([]db.DimPeriod, error) {
	return s.Queries.ListPeriods(ctx)
}
