import { api } from './client'

export type UserRole = 'field_user' | 'office_admin'

export interface User {
  id: number
  email: string
  name: string
  role: UserRole
  is_active: boolean
}

export const usersApi = {
  list: () => api.get<User[]>('/users'),
}
