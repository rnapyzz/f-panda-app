package inputsheet

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

// --- sheets ---

type sheetDTO struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

type sheetDetailDTO struct {
	ID       uint64          `json:"id"`
	Name     string          `json:"name"`
	Snapshot json.RawMessage `json:"snapshot"`
}

func (s *Service) ListSheetsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := s.ListSheetsByOwner(r.Context(), user.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]sheetDTO, len(rows))
	for i, row := range rows {
		out[i] = sheetDTO{ID: row.ID, Name: row.Name}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) CreateSheetHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name     string          `json:"name"`
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || len(req.Snapshot) == 0 {
		http.Error(w, "name and snapshot are required", http.StatusBadRequest)
		return
	}
	id, err := s.CreateSheet(r.Context(), user.ID, req.Name, req.Snapshot)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]uint64{"id": uint64(id)})
}

func (s *Service) GetSheetHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, ok := httpx.PathID(w, r, "id")
	if !ok {
		return
	}
	sheet, err := s.GetSheet(r.Context(), id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sheet.OwnerUserID != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sheetDetailDTO{ID: sheet.ID, Name: sheet.Name, Snapshot: json.RawMessage(sheet.SheetSnapshot)})
}

func (s *Service) UpdateSheetHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, ok := httpx.PathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Snapshot json.RawMessage `json:"snapshot"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Snapshot) == 0 {
		http.Error(w, "snapshot is required", http.StatusBadRequest)
		return
	}
	if err := s.UpdateSheetSnapshot(r.Context(), id, user.ID, req.Snapshot); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- bindings ---

type fixedDimensionsDTO struct {
	BusinessID   *uint64 `json:"business_id,omitempty"`
	DepartmentID *uint64 `json:"department_id,omitempty"`
	AccountID    *uint64 `json:"account_id,omitempty"`
	PeriodID     *uint64 `json:"period_id,omitempty"`
}

type axisLabelDTO struct {
	Axis                  string `json:"axis"`
	AxisIndex             int32  `json:"axis_index"`
	RawLabelText          string `json:"raw_label_text"`
	ResolvedDimensionType string `json:"resolved_dimension_type"`
	ResolvedDimensionID   uint64 `json:"resolved_dimension_id"`
}

type bindingDTO struct {
	ID               uint64             `json:"id"`
	Name             string             `json:"name"`
	RangeSheetName   string             `json:"range_sheet_name"`
	StartRow         int32              `json:"start_row"`
	EndRow           int32              `json:"end_row"`
	StartCol         int32              `json:"start_col"`
	EndCol           int32              `json:"end_col"`
	HeaderRows       int8               `json:"header_rows"`
	HeaderCols       int8               `json:"header_cols"`
	RowAxisDimension string             `json:"row_axis_dimension"`
	ColAxisDimension string             `json:"col_axis_dimension"`
	FixedDimensions  fixedDimensionsDTO `json:"fixed_dimensions"`
}

func (s *Service) ListBindingsHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sheetID, ok := httpx.PathID(w, r, "id")
	if !ok {
		return
	}
	sheet, err := s.GetSheet(r.Context(), sheetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sheet.OwnerUserID != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	rows, err := s.ListBindingsBySheet(r.Context(), sheetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]bindingDTO, len(rows))
	for i, row := range rows {
		var fixed fixedDimensionsDTO
		_ = json.Unmarshal(row.FixedDimensions, &fixed)
		out[i] = bindingDTO{
			ID: row.ID, Name: row.Name, RangeSheetName: row.RangeSheetName,
			StartRow: row.StartRow, EndRow: row.EndRow, StartCol: row.StartCol, EndCol: row.EndCol,
			HeaderRows: row.HeaderRows, HeaderCols: row.HeaderCols,
			RowAxisDimension: string(row.RowAxisDimension), ColAxisDimension: string(row.ColAxisDimension),
			FixedDimensions: fixed,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) CreateBindingHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	sheetID, ok := httpx.PathID(w, r, "id")
	if !ok {
		return
	}
	sheet, err := s.GetSheet(r.Context(), sheetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sheet.OwnerUserID != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Name             string             `json:"name"`
		RangeSheetName   string             `json:"range_sheet_name"`
		StartRow         int32              `json:"start_row"`
		EndRow           int32              `json:"end_row"`
		StartCol         int32              `json:"start_col"`
		EndCol           int32              `json:"end_col"`
		HeaderRows       int8               `json:"header_rows"`
		HeaderCols       int8               `json:"header_cols"`
		RowAxisDimension string             `json:"row_axis_dimension"`
		ColAxisDimension string             `json:"col_axis_dimension"`
		FixedDimensions  fixedDimensionsDTO `json:"fixed_dimensions"`
		AxisLabels       []axisLabelDTO     `json:"axis_labels"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.RangeSheetName == "" {
		http.Error(w, "name and range_sheet_name are required", http.StatusBadRequest)
		return
	}

	labels := make([]AxisLabelInput, len(req.AxisLabels))
	for i, l := range req.AxisLabels {
		labels[i] = AxisLabelInput{
			Axis: l.Axis, AxisIndex: l.AxisIndex, RawLabelText: l.RawLabelText,
			ResolvedDimensionType: l.ResolvedDimensionType, ResolvedDimensionID: l.ResolvedDimensionID,
		}
	}

	id, err := s.CreateBinding(r.Context(), CreateBindingInput{
		InputSheetID:     sheetID,
		Name:             req.Name,
		RangeSheetName:   req.RangeSheetName,
		StartRow:         req.StartRow,
		EndRow:           req.EndRow,
		StartCol:         req.StartCol,
		EndCol:           req.EndCol,
		HeaderRows:       req.HeaderRows,
		HeaderCols:       req.HeaderCols,
		RowAxisDimension: req.RowAxisDimension,
		ColAxisDimension: req.ColAxisDimension,
		FixedDimensions: FixedDimensions{
			BusinessID: req.FixedDimensions.BusinessID, DepartmentID: req.FixedDimensions.DepartmentID,
			AccountID: req.FixedDimensions.AccountID, PeriodID: req.FixedDimensions.PeriodID,
		},
		AxisLabels: labels,
		CreatedBy:  user.ID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]uint64{"id": uint64(id)})
}

// --- submissions ---

type submitResponseDTO struct {
	SubmissionID     int64             `json:"submission_id"`
	ValidationStatus string            `json:"validation_status"`
	Issues           []ValidationIssue `json:"issues"`
	FactRowsWritten  int               `json:"fact_rows_written"`
}

func (s *Service) SubmitHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		BindingID         uint64 `json:"binding_id"`
		ScenarioVersionID uint64 `json:"scenario_version_id"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.BindingID == 0 || req.ScenarioVersionID == 0 {
		http.Error(w, "binding_id and scenario_version_id are required", http.StatusBadRequest)
		return
	}

	detail, err := s.GetBindingDetail(r.Context(), req.BindingID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	sheet, err := s.GetSheet(r.Context(), detail.Binding.InputSheetID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if sheet.OwnerUserID != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	result, err := s.Submit(r.Context(), SubmitInput{
		BindingID:         req.BindingID,
		ScenarioVersionID: req.ScenarioVersionID,
		SubmittedBy:       user.ID,
	})
	if err != nil {
		if errors.Is(err, errIncompleteDimensionKey) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrScenarioVersionLocked) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeInternalError(w, err)
		return
	}

	issues := result.Issues
	if issues == nil {
		issues = []ValidationIssue{}
	}
	httpx.WriteJSON(w, http.StatusOK, submitResponseDTO{
		SubmissionID:     result.SubmissionID,
		ValidationStatus: result.ValidationStatus,
		Issues:           issues,
		FactRowsWritten:  result.FactRowsWritten,
	})
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("inputsheet handler error", "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
