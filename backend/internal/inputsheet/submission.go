package inputsheet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

var errIncompleteDimensionKey = errors.New("inputsheet: incomplete dimension key — check fixed_dimensions")
var ErrScenarioVersionLocked = errors.New("inputsheet: scenario version is locked")

type SubmitInput struct {
	BindingID         uint64
	ScenarioVersionID uint64
	SubmittedBy       uint64
}

// ValidationIssue describes one axis label that no longer matches the sheet
// — the minimal "shape check" safety net for Phase 2: if a field user
// reorganizes their sheet after a binding was defined, resubmitting must
// fail loudly rather than silently writing numbers against the wrong
// account/period/business/department.
type ValidationIssue struct {
	Axis      string `json:"axis"`
	AxisIndex int32  `json:"axis_index"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
}

type SubmitResult struct {
	SubmissionID     int64
	ValidationStatus string
	Issues           []ValidationIssue
	FactRowsWritten  int
}

func setDim(fd *FixedDimensions, dimType string, id uint64) {
	switch dimType {
	case "business":
		fd.BusinessID = &id
	case "department":
		fd.DepartmentID = &id
	case "account":
		fd.AccountID = &id
	case "period":
		fd.PeriodID = &id
	}
}

// Submit re-reads the sheet's currently saved snapshot (not any
// in-browser-only state — the sheet must have been saved first),
// shape-checks it against the binding's axis labels, and — only if the
// shape still matches — writes one fact_amount row per non-blank data cell
// in the bound range.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitResult, error) {
	version, err := s.Queries.GetScenarioVersionByID(ctx, in.ScenarioVersionID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("load scenario version: %w", err)
	}
	if version.Status == db.ScenarioVersionStatusLocked {
		return SubmitResult{}, ErrScenarioVersionLocked
	}

	detail, err := s.GetBindingDetail(ctx, in.BindingID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("load binding: %w", err)
	}
	binding := detail.Binding

	sheet, err := s.GetSheet(ctx, binding.InputSheetID)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("load sheet: %w", err)
	}

	wb, err := parseWorkbookSnapshot(sheet.SheetSnapshot)
	if err != nil {
		return SubmitResult{}, err
	}

	sheetData, ok := wb.findSheetByName(binding.RangeSheetName)
	if !ok {
		return SubmitResult{}, fmt.Errorf("sheet %q not found in the current snapshot", binding.RangeSheetName)
	}

	var fixed FixedDimensions
	if err := json.Unmarshal(binding.FixedDimensions, &fixed); err != nil {
		return SubmitResult{}, fmt.Errorf("parse fixed_dimensions: %w", err)
	}

	var issues []ValidationIssue
	rowLabels := map[int32]db.InputBindingAxisLabel{}
	colLabels := map[int32]db.InputBindingAxisLabel{}
	for _, label := range detail.Labels {
		switch label.Axis {
		case db.InputBindingAxisLabelAxisRow:
			rowLabels[label.AxisIndex] = label
			actual := sheetData.cellText(binding.StartRow+int32(binding.HeaderRows)+label.AxisIndex, binding.StartCol+int32(binding.HeaderCols)-1)
			if actual != label.RawLabelText {
				issues = append(issues, ValidationIssue{Axis: "row", AxisIndex: label.AxisIndex, Expected: label.RawLabelText, Actual: actual})
			}
		case db.InputBindingAxisLabelAxisCol:
			colLabels[label.AxisIndex] = label
			actual := sheetData.cellText(binding.StartRow+int32(binding.HeaderRows)-1, binding.StartCol+int32(binding.HeaderCols)+label.AxisIndex)
			if actual != label.RawLabelText {
				issues = append(issues, ValidationIssue{Axis: "col", AxisIndex: label.AxisIndex, Expected: label.RawLabelText, Actual: actual})
			}
		}
	}

	validationStatus := db.SubmissionValidationStatusOk
	if len(issues) > 0 {
		validationStatus = db.SubmissionValidationStatusError
	}

	// Always write a valid JSON value (never a SQL NULL): database/sql
	// cannot Scan a NULL column back into *json.RawMessage, so a later
	// read (e.g. ListSubmissionsBySheet) would fail with "unsupported
	// Scan ... into *json.RawMessage".
	validationDetail := json.RawMessage("null")
	if len(issues) > 0 {
		validationDetail, err = json.Marshal(issues)
		if err != nil {
			return SubmitResult{}, err
		}
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return SubmitResult{}, err
	}
	defer tx.Rollback()
	qtx := s.Queries.WithTx(tx)

	submissionID, err := qtx.CreateSubmission(ctx, db.CreateSubmissionParams{
		InputSheetID:      binding.InputSheetID,
		BindingID:         in.BindingID,
		ScenarioVersionID: in.ScenarioVersionID,
		SubmittedBy:       in.SubmittedBy,
		Status:            db.SubmissionStatusSubmitted,
		ValidationStatus:  validationStatus,
		ValidationDetail:  validationDetail,
	})
	if err != nil {
		return SubmitResult{}, fmt.Errorf("create submission: %w", err)
	}

	factRowsWritten := 0
	if len(issues) == 0 {
		for r := binding.StartRow + int32(binding.HeaderRows); r <= binding.EndRow; r++ {
			rowIdx := r - binding.StartRow - int32(binding.HeaderRows)
			rowLabel, ok := rowLabels[rowIdx]
			if !ok {
				continue
			}
			for c := binding.StartCol + int32(binding.HeaderCols); c <= binding.EndCol; c++ {
				colIdx := c - binding.StartCol - int32(binding.HeaderCols)
				colLabel, ok := colLabels[colIdx]
				if !ok {
					continue
				}

				amount, hasValue := cellToFloat(sheetData.cellValue(r, c))
				if !hasValue {
					continue
				}

				key := fixed
				setDim(&key, string(binding.RowAxisDimension), rowLabel.ResolvedDimensionID)
				setDim(&key, string(binding.ColAxisDimension), colLabel.ResolvedDimensionID)

				if key.BusinessID == nil || key.DepartmentID == nil || key.AccountID == nil || key.PeriodID == nil {
					return SubmitResult{}, fmt.Errorf("binding %d: %w (row %d col %d)", binding.ID, errIncompleteDimensionKey, r, c)
				}

				if err := qtx.UpsertFactAmountFromSubmission(ctx, db.UpsertFactAmountFromSubmissionParams{
					ScenarioVersionID: in.ScenarioVersionID,
					BusinessID:        *key.BusinessID,
					DepartmentID:      *key.DepartmentID,
					AccountID:         *key.AccountID,
					PeriodID:          *key.PeriodID,
					Amount:            strconv.FormatFloat(amount, 'f', 2, 64),
					SubmissionID:      sql.NullInt64{Int64: submissionID, Valid: true},
					CreatedBy:         in.SubmittedBy,
				}); err != nil {
					return SubmitResult{}, fmt.Errorf("write fact amount: %w", err)
				}
				factRowsWritten++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return SubmitResult{}, err
	}

	return SubmitResult{
		SubmissionID:     submissionID,
		ValidationStatus: string(validationStatus),
		Issues:           issues,
		FactRowsWritten:  factRowsWritten,
	}, nil
}

func (s *Service) ListSubmissionsBySheet(ctx context.Context, sheetID uint64) ([]db.Submission, error) {
	return s.Queries.ListSubmissionsBySheet(ctx, sheetID)
}
