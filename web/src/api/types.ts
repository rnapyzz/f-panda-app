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

// --- 施策 ---

export type ActivityType = 'project' | 'recurring' | 'cost_pool'
export type ActivityStatus = 'planned' | 'in_progress' | 'completed' | 'on_hold' | 'cancelled'
export type CalcMode = 'manual' | 'formula'

export type Activity = Timestamps & {
  id: number
  function_id: number
  code: string
  name: string
  activity_type: ActivityType
  status: ActivityStatus
  start_date: string | null
  end_date: string | null
  owner_user_id: number | null
  calc_mode: CalcMode
  probability: number | null
  assumptions: string
  can_edit: boolean
}

export type MilestoneStatus = 'not_started' | 'in_progress' | 'completed' | 'delayed'

export type Milestone = Timestamps & {
  id: number
  activity_id: number
  name: string
  due_date: string
  status: MilestoneStatus
}

export type DriverKind = 'value' | 'cost' | 'kpi'

export type Driver = Timestamps & {
  id: number
  activity_id: number
  code: string
  name: string
  driver_kind: DriverKind
  unit: string
}

export type Formula = Timestamps & {
  id: number
  activity_id: number
  subject_id: number
  expression: string
}

export type ActivityDetail = Activity & {
  milestones: Milestone[]
  drivers: Driver[]
  formulas: Formula[]
}

export const activityTypeLabels: Record<ActivityType, string> = {
  project: 'プロジェクト型',
  recurring: '運用型',
  cost_pool: 'コストプール型',
}

export const activityStatusLabels: Record<ActivityStatus, string> = {
  planned: '計画中',
  in_progress: '実行中',
  completed: '完了',
  on_hold: '保留',
  cancelled: '中止',
}

export const calcModeLabels: Record<CalcMode, string> = {
  manual: '直接入力',
  formula: '計算式',
}

export const milestoneStatusLabels: Record<MilestoneStatus, string> = {
  not_started: '未着手',
  in_progress: '進行中',
  completed: '完了',
  delayed: '遅延',
}

export const driverKindLabels: Record<DriverKind, string> = {
  value: 'バリュードライバー',
  cost: 'コストドライバー',
  kpi: 'KPI',
}

// --- シナリオ ---

export type ScenarioKind = 'budget' | 'forecast' | 'actual' | 'optimistic' | 'pessimistic' | 'other'

export type Scenario = Timestamps & {
  id: number
  name: string
  scenario_kind: ScenarioKind
  fiscal_year: number
  base_scenario_id: number | null
  is_locked: boolean
  created_by: number
}

export const scenarioKindLabels: Record<ScenarioKind, string> = {
  budget: '予算',
  forecast: '見込',
  actual: '実績',
  optimistic: '楽観',
  pessimistic: '悲観',
  other: 'その他',
}

export type ValueCell = {
  target_month: string
  value: number | string
  is_provisional: boolean
  provisional_reason: string
}

export type DriverRow = {
  id: number
  code: string
  name: string
  driver_kind: DriverKind
  unit: string
  values: ValueCell[]
}

export type AmountCell = {
  target_month: string
  amount: number | string
  source: 'manual' | 'formula' | 'import'
  is_provisional: boolean
  provisional_reason: string
}

export type AmountRow = {
  subject_id: number
  code: string
  name: string
  category: SubjectCategory
  has_formula: boolean
  values: AmountCell[]
}

export type ActivitySummary = {
  id: number
  code: string
  name: string
  calc_mode: CalcMode
  probability: number | null
  can_edit: boolean
}

/** GET /scenarios/{id}/activities/{aid} */
export type ValuesView = {
  scenario: Scenario
  activity: ActivitySummary
  months: string[]
  editable: boolean
  drivers: DriverRow[]
  amounts: AmountRow[]
  condition: string | null
}

export type ImportResult = {
  dry_run: boolean
  months: string[]
  rows: number
  facts: number
  inserted: number
  updated: number
  deleted: number
  unchanged: number
  totals: { month: string; revenue: string; expense: string }[]
}
