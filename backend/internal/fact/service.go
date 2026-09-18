// Package fact implements manual entry into fact_amount and the budget vs
// forecast vs actual variance report — the part of Phase 1 that proves the
// canonical dimensional schema is actually useful before any investment
// goes into the free-form spreadsheet input layer.
package fact

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
	"github.com/rnapyzz/f-panda-app/backend/internal/scenario"
)

type Service struct {
	Queries   *db.Queries
	Scenarios *scenario.Service
}

func NewService(q *db.Queries, scenarios *scenario.Service) *Service {
	return &Service{Queries: q, Scenarios: scenarios}
}

type UpsertEntryInput struct {
	ScenarioVersionID uint64
	BusinessID        uint64
	DepartmentID      uint64
	AccountID         uint64
	PeriodID          uint64
	Amount            float64
	CreatedBy         uint64
}

func (s *Service) UpsertEntry(ctx context.Context, in UpsertEntryInput) error {
	return s.Queries.UpsertFactAmount(ctx, db.UpsertFactAmountParams{
		ScenarioVersionID: in.ScenarioVersionID,
		BusinessID:        in.BusinessID,
		DepartmentID:      in.DepartmentID,
		AccountID:         in.AccountID,
		PeriodID:          in.PeriodID,
		Amount:            strconv.FormatFloat(in.Amount, 'f', 2, 64),
		CreatedBy:         in.CreatedBy,
	})
}

type Filter struct {
	BusinessID   *uint64
	DepartmentID *uint64
	AccountID    *uint64
}

type VarianceRow struct {
	BusinessID     uint64   `json:"business_id"`
	BusinessCode   string   `json:"business_code"`
	BusinessName   string   `json:"business_name"`
	DepartmentID   uint64   `json:"department_id"`
	DepartmentCode string   `json:"department_code"`
	DepartmentName string   `json:"department_name"`
	AccountID      uint64   `json:"account_id"`
	AccountCode    string   `json:"account_code"`
	AccountName    string   `json:"account_name"`
	PeriodID       uint64   `json:"period_id"`
	PeriodLabel    string   `json:"period_label"`
	FiscalMonth    int8     `json:"fiscal_month"`
	BudgetAmount   *float64 `json:"budget_amount"`
	ForecastAmount *float64 `json:"forecast_amount"`
	ActualAmount   *float64 `json:"actual_amount"`
	VarianceAmount *float64 `json:"variance_amount"`
}

type dimKey struct {
	businessID, departmentID, accountID, periodID uint64
}

type cell struct {
	budget, forecast, actual *float64
}

func (s *Service) VarianceReport(ctx context.Context, fiscalYear int16, filter Filter) ([]VarianceRow, error) {
	budgetVer, err := s.Scenarios.Current(ctx, db.ScenarioVersionScenarioTypeBudget, fiscalYear)
	if err != nil {
		return nil, fmt.Errorf("current budget version: %w", err)
	}
	forecastVer, err := s.Scenarios.Current(ctx, db.ScenarioVersionScenarioTypeForecast, fiscalYear)
	if err != nil {
		return nil, fmt.Errorf("current forecast version: %w", err)
	}
	actualVer, err := s.Scenarios.Current(ctx, db.ScenarioVersionScenarioTypeActual, fiscalYear)
	if err != nil {
		return nil, fmt.Errorf("current actual version: %w", err)
	}

	cells := map[dimKey]*cell{}
	load := func(ver *db.ScenarioVersion, assign func(c *cell, amt float64)) error {
		if ver == nil {
			return nil
		}
		rows, err := s.Queries.ListFactAmountsByScenarioVersion(ctx, ver.ID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			amt, err := strconv.ParseFloat(row.Amount, 64)
			if err != nil {
				return fmt.Errorf("parse amount %q: %w", row.Amount, err)
			}
			key := dimKey{row.BusinessID, row.DepartmentID, row.AccountID, row.PeriodID}
			c, ok := cells[key]
			if !ok {
				c = &cell{}
				cells[key] = c
			}
			assign(c, amt)
		}
		return nil
	}

	if err := load(budgetVer, func(c *cell, amt float64) { c.budget = &amt }); err != nil {
		return nil, fmt.Errorf("load budget facts: %w", err)
	}
	if err := load(forecastVer, func(c *cell, amt float64) { c.forecast = &amt }); err != nil {
		return nil, fmt.Errorf("load forecast facts: %w", err)
	}
	if err := load(actualVer, func(c *cell, amt float64) { c.actual = &amt }); err != nil {
		return nil, fmt.Errorf("load actual facts: %w", err)
	}

	businesses, err := s.Queries.ListBusinesses(ctx)
	if err != nil {
		return nil, err
	}
	departments, err := s.Queries.ListDepartments(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := s.Queries.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	periods, err := s.Queries.ListPeriods(ctx)
	if err != nil {
		return nil, err
	}

	businessByID := make(map[uint64]db.DimBusiness, len(businesses))
	for _, b := range businesses {
		businessByID[b.ID] = b
	}
	departmentByID := make(map[uint64]db.DimDepartment, len(departments))
	for _, d := range departments {
		departmentByID[d.ID] = d
	}
	accountByID := make(map[uint64]db.DimAccount, len(accounts))
	for _, a := range accounts {
		accountByID[a.ID] = a
	}
	periodByID := make(map[uint64]db.DimPeriod, len(periods))
	for _, p := range periods {
		periodByID[p.ID] = p
	}

	out := make([]VarianceRow, 0, len(cells))
	for key, c := range cells {
		if filter.BusinessID != nil && key.businessID != *filter.BusinessID {
			continue
		}
		if filter.DepartmentID != nil && key.departmentID != *filter.DepartmentID {
			continue
		}
		if filter.AccountID != nil && key.accountID != *filter.AccountID {
			continue
		}

		var variance *float64
		if c.budget != nil && c.actual != nil {
			v := *c.actual - *c.budget
			variance = &v
		}

		b := businessByID[key.businessID]
		d := departmentByID[key.departmentID]
		a := accountByID[key.accountID]
		p := periodByID[key.periodID]

		out = append(out, VarianceRow{
			BusinessID:     key.businessID,
			BusinessCode:   b.Code,
			BusinessName:   b.Name,
			DepartmentID:   key.departmentID,
			DepartmentCode: d.Code,
			DepartmentName: d.Name,
			AccountID:      key.accountID,
			AccountCode:    a.Code,
			AccountName:    a.Name,
			PeriodID:       key.periodID,
			PeriodLabel:    p.Label,
			FiscalMonth:    p.FiscalMonth,
			BudgetAmount:   c.budget,
			ForecastAmount: c.forecast,
			ActualAmount:   c.actual,
			VarianceAmount: variance,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].BusinessCode != out[j].BusinessCode {
			return out[i].BusinessCode < out[j].BusinessCode
		}
		if out[i].DepartmentCode != out[j].DepartmentCode {
			return out[i].DepartmentCode < out[j].DepartmentCode
		}
		if out[i].AccountCode != out[j].AccountCode {
			return out[i].AccountCode < out[j].AccountCode
		}
		return out[i].FiscalMonth < out[j].FiscalMonth
	})

	return out, nil
}
