import { api } from './client'

export interface VarianceRow {
  business_id: number
  business_code: string
  business_name: string
  department_id: number
  department_code: string
  department_name: string
  account_id: number
  account_code: string
  account_name: string
  period_id: number
  period_label: string
  fiscal_month: number
  budget_amount: number | null
  forecast_amount: number | null
  actual_amount: number | null
  variance_amount: number | null
}

export const factsApi = {
  upsertEntry: (input: {
    scenario_version_id: number
    business_id: number
    department_id: number
    account_id: number
    period_id: number
    amount: number
  }) => api.post<void>('/fact-entries', input),

  varianceReport: (fiscalYear: number, filter?: { businessId?: number; departmentId?: number; accountId?: number }) => {
    const params = new URLSearchParams({ fiscal_year: String(fiscalYear) })
    if (filter?.businessId) params.set('business_id', String(filter.businessId))
    if (filter?.departmentId) params.set('department_id', String(filter.departmentId))
    if (filter?.accountId) params.set('account_id', String(filter.accountId))
    return api.get<VarianceRow[]>(`/variance-report?${params.toString()}`)
  },
}
