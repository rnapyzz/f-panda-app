import { api } from './client'

export interface Period {
  id: number
  fiscal_year: number
  fiscal_month: number
  calendar_year: number
  calendar_month: number
  label: string
}

export const periodsApi = {
  list: () => api.get<Period[]>('/periods'),
}
