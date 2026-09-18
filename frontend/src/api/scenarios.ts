import { api } from './client'

export type ScenarioType = 'budget' | 'forecast' | 'actual'

export interface ScenarioVersion {
  id: number
  scenario_type: ScenarioType
  fiscal_year: number
  as_of_period_id?: number
  version_label: string
  status: 'draft' | 'submitted' | 'locked'
  is_current: boolean
  submitted_at?: string
  locked_at?: string
}

export const scenarioVersionsApi = {
  list: (scenarioType: ScenarioType, fiscalYear: number) =>
    api.get<ScenarioVersion[]>(
      `/scenario-versions?scenario_type=${scenarioType}&fiscal_year=${fiscalYear}`,
    ),
  create: (input: {
    scenario_type: ScenarioType
    fiscal_year: number
    as_of_period_id?: number
    version_label: string
  }) => api.post<{ id: number }>('/scenario-versions', input),
  submit: (id: number) => api.post<void>(`/scenario-versions/${id}/submit`),
  lock: (id: number) => api.post<void>(`/scenario-versions/${id}/lock`),
}
