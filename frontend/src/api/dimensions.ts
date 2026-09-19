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

export interface Service {
  id: number
  code: string
  name: string
  business_id: number
  is_active: boolean
}

export interface Project {
  id: number
  code: string
  name: string
  service_id: number
  primary_department_id: number
  is_active: boolean
}

export interface Initiative {
  id: number
  code: string
  name: string
  project_id: number
  primary_department_id: number
  is_active: boolean
}

export const servicesApi = {
  list: () => api.get<Service[]>('/services'),
  create: (input: { code: string; name: string; business_id: number }) => api.post<Service>('/services', input),
  update: (id: number, input: { code: string; name: string; business_id: number; is_active: boolean }) =>
    api.put<Service>(`/services/${id}`, input),
}

export const projectsApi = {
  list: () => api.get<Project[]>('/projects'),
  create: (input: { code: string; name: string; service_id: number; primary_department_id: number }) =>
    api.post<Project>('/projects', input),
  update: (
    id: number,
    input: { code: string; name: string; service_id: number; primary_department_id: number; is_active: boolean },
  ) => api.put<Project>(`/projects/${id}`, input),
}

export const initiativesApi = {
  list: () => api.get<Initiative[]>('/initiatives'),
  create: (input: { code: string; name: string; project_id: number; primary_department_id: number }) =>
    api.post<Initiative>('/initiatives', input),
  update: (
    id: number,
    input: { code: string; name: string; project_id: number; primary_department_id: number; is_active: boolean },
  ) => api.put<Initiative>(`/initiatives/${id}`, input),
}
