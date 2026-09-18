import { api } from './client'

export interface PreviewResult {
  headers: string[]
  sample_rows: string[][]
  total_rows: number
}

export interface ColumnMapping {
  business_column: number
  department_column: number
  account_column: number
  period_column: number
  amount_column: number
}

export interface RowError {
  row_number: number
  message: string
}

export interface CommitResult {
  import_batch_id: number
  status: 'processing' | 'completed' | 'failed'
  row_count: number
  error_count: number
  errors: RowError[]
}

export interface ImportBatch {
  id: number
  original_filename: string
  file_size_bytes: number
  status: 'processing' | 'completed' | 'failed'
  row_count: number
  error_count: number
  created_at: string
  completed_at?: string
}

export const importApi = {
  preview: (file: File) => {
    const form = new FormData()
    form.set('file', file)
    return api.postForm<PreviewResult>('/import-batches/preview', form)
  },

  commit: (file: File, scenarioVersionId: number, mapping: ColumnMapping) => {
    const form = new FormData()
    form.set('file', file)
    form.set('scenario_version_id', String(scenarioVersionId))
    form.set('mapping', JSON.stringify(mapping))
    return api.postForm<CommitResult>('/import-batches', form)
  },

  listBatches: (scenarioVersionId: number) => api.get<ImportBatch[]>(`/import-batches?scenario_version_id=${scenarioVersionId}`),
}
