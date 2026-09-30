// API のリソース型。api/internal の各 JSON と対応する。

export type Role = 'fpa_admin' | 'manager' | 'member' | 'viewer'

export type CurrentUser = {
  id: number
  name: string
  email: string
  role: Role
}

export type List<T> = { items: T[] }

type Timestamps = { created_at: string; updated_at: string }

export type TreeNode = Timestamps & {
  id: number
  parent_id: number | null
  name: string
  level: number
  sort_order: number
}

export type FunctionItem = Timestamps & {
  id: number
  name: string
  segment_id: number
  organization_id: number
  owner_user_id: number | null
}

export type SubjectCategory = 'revenue' | 'expense'

export type Subject = Timestamps & {
  id: number
  parent_id: number | null
  code: string
  name: string
  category: SubjectCategory
  sort_order: number
}

export type User = Timestamps & {
  id: number
  name: string
  email: string
  role: Role
  is_active: boolean
}

export const roleLabels: Record<Role, string> = {
  fpa_admin: 'FP&A',
  manager: 'マネージャー',
  member: '担当者',
  viewer: '閲覧者',
}

export const categoryLabels: Record<SubjectCategory, string> = {
  revenue: '収益',
  expense: '費用',
}
