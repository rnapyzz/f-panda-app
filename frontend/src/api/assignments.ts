import { api } from './client'

export interface UserAssignment {
  id: number
  user_id: number
  user_name: string
  user_email: string
  business_id: number
  business_code: string
  business_name: string
  department_id: number
  department_code: string
  department_name: string
}

export const assignmentsApi = {
  list: () => api.get<UserAssignment[]>('/user-assignments'),
  create: (input: { user_id: number; business_id: number; department_id: number }) =>
    api.post<{ id: number }>('/user-assignments', input),
  deactivate: (id: number) => api.del<void>(`/user-assignments/${id}`),
}
