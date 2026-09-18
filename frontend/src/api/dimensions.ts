import { api } from './client'

export interface Business {
  id: number
  code: string
  name: string
  is_active: boolean
}

export interface Department {
  id: number
  code: string
  name: string
  is_active: boolean
}

export type AccountType = 'revenue' | 'cost' | 'other'

export interface Account {
  id: number
  code: string
  name: string
  account_type: AccountType
  is_active: boolean
}

export const businessesApi = {
  list: () => api.get<Business[]>('/businesses'),
  create: (input: { code: string; name: string }) => api.post<Business>('/businesses', input),
  update: (id: number, input: { code: string; name: string; is_active: boolean }) =>
    api.put<Business>(`/businesses/${id}`, input),
}

export const departmentsApi = {
  list: () => api.get<Department[]>('/departments'),
  create: (input: { code: string; name: string }) => api.post<Department>('/departments', input),
  update: (id: number, input: { code: string; name: string; is_active: boolean }) =>
    api.put<Department>(`/departments/${id}`, input),
}

export const accountsApi = {
  list: () => api.get<Account[]>('/accounts'),
  create: (input: { code: string; name: string; account_type: AccountType }) =>
    api.post<Account>('/accounts', input),
  update: (id: number, input: { code: string; name: string; account_type: AccountType; is_active: boolean }) =>
    api.put<Account>(`/accounts/${id}`, input),
}
