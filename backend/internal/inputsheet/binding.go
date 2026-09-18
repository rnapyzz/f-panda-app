package inputsheet

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

// FixedDimensions holds whichever of the four dimensions aren't covered by
// the binding's row/col axes — e.g. if row=account and col=period, business
// and department must be fixed to a single value for the whole range.
type FixedDimensions struct {
	BusinessID   *uint64 `json:"business_id,omitempty"`
	DepartmentID *uint64 `json:"department_id,omitempty"`
	AccountID    *uint64 `json:"account_id,omitempty"`
	PeriodID     *uint64 `json:"period_id,omitempty"`
}

type AxisLabelInput struct {
	Axis                  string // "row" | "col"
	AxisIndex             int32
	RawLabelText          string
	ResolvedDimensionType string // "account" | "period" | "business" | "department"
	ResolvedDimensionID   uint64
}

type CreateBindingInput struct {
	InputSheetID     uint64
	Name             string
	RangeSheetName   string
	StartRow         int32
	EndRow           int32
	StartCol         int32
	EndCol           int32
	HeaderRows       int8
	HeaderCols       int8
	RowAxisDimension string // "account" | "period" | "business" | "department"
	ColAxisDimension string
	FixedDimensions  FixedDimensions
	AxisLabels       []AxisLabelInput
	CreatedBy        uint64
}

var validAxisDimensions = map[string]bool{
	"account": true, "period": true, "business": true, "department": true,
}

func (s *Service) CreateBinding(ctx context.Context, in CreateBindingInput) (int64, error) {
	if !validAxisDimensions[in.RowAxisDimension] || !validAxisDimensions[in.ColAxisDimension] {
		return 0, fmt.Errorf("row_axis_dimension and col_axis_dimension must each be one of account/period/business/department")
	}
	if in.RowAxisDimension == in.ColAxisDimension {
		return 0, fmt.Errorf("row_axis_dimension and col_axis_dimension must be different")
	}

	fixedJSON, err := json.Marshal(in.FixedDimensions)
	if err != nil {
		return 0, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qtx := s.Queries.WithTx(tx)

	bindingID, err := qtx.CreateInputBinding(ctx, db.CreateInputBindingParams{
		InputSheetID:     in.InputSheetID,
		Name:             in.Name,
		RangeSheetName:   in.RangeSheetName,
		StartRow:         in.StartRow,
		EndRow:           in.EndRow,
		StartCol:         in.StartCol,
		EndCol:           in.EndCol,
		HeaderRows:       in.HeaderRows,
		HeaderCols:       in.HeaderCols,
		RowAxisDimension: db.InputBindingRowAxisDimension(in.RowAxisDimension),
		ColAxisDimension: db.InputBindingColAxisDimension(in.ColAxisDimension),
		FixedDimensions:  fixedJSON,
		CreatedBy:        in.CreatedBy,
	})
	if err != nil {
		return 0, err
	}

	for _, label := range in.AxisLabels {
		if !validAxisDimensions[label.ResolvedDimensionType] {
			return 0, fmt.Errorf("axis label resolved_dimension_type %q is invalid", label.ResolvedDimensionType)
		}
		if err := qtx.CreateInputBindingAxisLabel(ctx, db.CreateInputBindingAxisLabelParams{
			BindingID:             uint64(bindingID),
			Axis:                  db.InputBindingAxisLabelAxis(label.Axis),
			AxisIndex:             label.AxisIndex,
			RawLabelText:          label.RawLabelText,
			ResolvedDimensionType: db.InputBindingAxisLabelResolvedDimensionType(label.ResolvedDimensionType),
			ResolvedDimensionID:   label.ResolvedDimensionID,
		}); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return bindingID, nil
}

func (s *Service) ListBindingsBySheet(ctx context.Context, sheetID uint64) ([]db.InputBinding, error) {
	return s.Queries.ListInputBindingsBySheet(ctx, sheetID)
}

type BindingDetail struct {
	Binding db.InputBinding
	Labels  []db.InputBindingAxisLabel
}

func (s *Service) GetBindingDetail(ctx context.Context, bindingID uint64) (BindingDetail, error) {
	binding, err := s.Queries.GetInputBinding(ctx, bindingID)
	if err != nil {
		return BindingDetail{}, err
	}
	labels, err := s.Queries.ListAxisLabelsByBinding(ctx, bindingID)
	if err != nil {
		return BindingDetail{}, err
	}
	return BindingDetail{Binding: binding, Labels: labels}, nil
}
