import { api } from './client'

export interface SheetSummary {
  id: number
  name: string
}

export interface SheetDetail {
  id: number
  name: string
  snapshot: unknown
}

export type AxisDimension = 'account' | 'period' | 'business' | 'department'

export interface FixedDimensions {
  business_id?: number
  department_id?: number
  account_id?: number
  period_id?: number
}

export interface AxisLabelInput {
  axis: 'row' | 'col'
  axis_index: number
  raw_label_text: string
  resolved_dimension_type: AxisDimension
  resolved_dimension_id: number
}

export interface Binding {
  id: number
  name: string
  range_sheet_name: string
  start_row: number
  end_row: number
  start_col: number
  end_col: number
  header_rows: number
  header_cols: number
  row_axis_dimension: AxisDimension
  col_axis_dimension: AxisDimension
  fixed_dimensions: FixedDimensions
}

export interface CreateBindingRequest {
  name: string
  range_sheet_name: string
  start_row: number
  end_row: number
  start_col: number
  end_col: number
  header_rows: number
  header_cols: number
  row_axis_dimension: AxisDimension
  col_axis_dimension: AxisDimension
  fixed_dimensions: FixedDimensions
  axis_labels: AxisLabelInput[]
}

export interface SubmitResult {
  submission_id: number
  validation_status: 'ok' | 'warning' | 'error'
  issues: Array<{ axis: 'row' | 'col'; axis_index: number; expected: string; actual: string }>
  fact_rows_written: number
}

export const sheetsApi = {
  list: () => api.get<SheetSummary[]>('/sheets'),
  create: (name: string, snapshot: unknown) => api.post<{ id: number }>('/sheets', { name, snapshot }),
  get: (id: number) => api.get<SheetDetail>(`/sheets/${id}`),
  updateSnapshot: (id: number, snapshot: unknown) => api.put<void>(`/sheets/${id}`, { snapshot }),

  listBindings: (sheetId: number) => api.get<Binding[]>(`/sheets/${sheetId}/bindings`),
  createBinding: (sheetId: number, input: CreateBindingRequest) =>
    api.post<{ id: number }>(`/sheets/${sheetId}/bindings`, input),
}

export const submissionsApi = {
  submit: (bindingId: number, scenarioVersionId: number) =>
    api.post<SubmitResult>('/submissions', { binding_id: bindingId, scenario_version_id: scenarioVersionId }),
}

export interface ReviewSubmission {
  id: number
  sheet_name: string
  owner_name: string
  binding_name: string
  scenario_type: string
  fiscal_year: number
  version_label: string
  submitted_by_name: string
  submitted_at: string
  status: 'draft' | 'submitted' | 'superseded'
  validation_status: 'ok' | 'warning' | 'error'
  validation_detail: Array<{ axis: 'row' | 'col'; axis_index: number; expected: string; actual: string }> | null
}

export const submissionReviewApi = {
  list: () => api.get<ReviewSubmission[]>('/submissions'),
}
