import { api } from './client'

export interface AuditLogEntry {
  id: number
  user_id?: number
  user_name?: string
  user_email?: string
  action: string
  entity_type: string
  entity_id?: number
  detail: unknown
  ip_address?: string
  created_at: string
}

export interface AuditLogFilter {
  user_id?: number
  action?: string
  entity_type?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export const auditLogApi = {
  list: (filter: AuditLogFilter = {}) => {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(filter)) {
      if (value !== undefined && value !== '') {
        params.set(key, String(value))
      }
    }
    const qs = params.toString()
    return api.get<AuditLogEntry[]>(`/audit-logs${qs ? `?${qs}` : ''}`)
  },
}
