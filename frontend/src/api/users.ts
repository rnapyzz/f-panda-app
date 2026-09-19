import { api } from './client'

export type UserRole = 'field_user' | 'office_admin'

export interface User {
  id: number
  email: string
  name: string
  role: UserRole
  department_id?: number
  is_active: boolean
}

export const usersApi = {
  list: () => api.get<User[]>('/users'),
  create: (input: { email: string; name: string; role: UserRole; department_id?: number; password: string }) =>
    api.post<User>('/users', input),
  update: (id: number, input: { name: string; role: UserRole; department_id?: number; is_active: boolean }) =>
    api.put<User>(`/users/${id}`, input),
  resetPassword: (id: number, password: string) => api.post<void>(`/users/${id}/reset-password`, { password }),
}
