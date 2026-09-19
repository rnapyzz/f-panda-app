export type UserRole = 'field_user' | 'office_admin'

export interface CurrentUser {
  id: number
  email: string
  name: string
  role: UserRole
  department_id?: number
}
