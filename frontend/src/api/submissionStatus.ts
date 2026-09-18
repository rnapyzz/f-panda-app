import { api } from './client'

export interface AssignedUser {
  user_id: number
  name: string
}

export interface ScopeStatus {
  business_id: number
  business_code: string
  business_name: string
  department_id: number
  department_code: string
  department_name: string
  assigned_users: AssignedUser[]
  submitted: boolean
  submission_id?: number
  validation_status?: 'ok' | 'warning' | 'error'
  submitted_at?: string
}

export const submissionStatusApi = {
  get: (scenarioVersionId: number) =>
    api.get<ScopeStatus[]>(`/submission-status?scenario_version_id=${scenarioVersionId}`),
}
