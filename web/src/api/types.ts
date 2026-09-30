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
  code: string
  name: string
  level: number
  sort_order: number
}

/** ユニットの種別。service = サービス（プロフィットセンター）、cost_center = 共通費、corporate = 管理部門 */
export type UnitType = 'service' | 'cost_center' | 'corporate'

export const unitTypeLabels: Record<UnitType, string> = {
  service: 'サービス',
  cost_center: '共通費',
  corporate: '管理部門',
}

/** ユニット（施策を束ねる単位） */
export type Unit = Timestamps & {
  id: number
  code: string
  name: string
  unit_type: UnitType
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
  /** パスワードが設定されているか（CSV で追加したユーザーは未設定） */
  has_password: boolean
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

export type Activity = Timestamps & {
  id: number
  unit_id: number
  code: string
  name: string
  activity_type: ActivityType
  status: ActivityStatus
  start_date: string | null
  end_date: string | null
  owner_user_id: number | null
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

/** 金額の内訳。施策 × 科目の下に複数持てる。formula_enabled なら計算式で算出した金額を反映する */
export type Line = Timestamps & {
  id: number
  activity_id: number
  subject_id: number
  name: string
  expression: string
  formula_enabled: boolean
  sort_order: number
}

export type ExternalCode = Timestamps & {
  id: number
  activity_id: number
  code: string
  note: string
}

export type ActivityDetail = Activity & {
  external_codes: ExternalCode[]
  milestones: Milestone[]
  drivers: Driver[]
  lines: Line[]
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

/** 内訳の金額。formula_enabled なら計算式で算出され、直接入力できない */
export type AmountLine = {
  id: number
  name: string
  expression: string
  formula_enabled: boolean
  values: AmountCell[]
}

/** 科目の金額。values は科目への直接入力（内訳なし）、lines は内訳ごと。科目の金額はそれらの合計 */
export type AmountRow = {
  subject_id: number
  code: string
  name: string
  category: SubjectCategory
  values: AmountCell[]
  lines: AmountLine[]
}

export type ActivitySummary = {
  id: number
  code: string
  name: string
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

// --- 予実比較 ---

export type ReportSeries = {
  key: string
  label: string
  kind: 'scenario' | 'landing'
  scenario_id?: number
  actual_scenario_id?: number
  forecast_scenario_id?: number
  actual_through?: string
}

export type ReportRow = {
  unit_id: number
  activity_id?: number
  subject_id: number
  month: string
  values: Record<string, string>
}

export type ComparisonReport = {
  fiscal_year: number
  months: string[]
  series: ReportSeries[]
  rows: ReportRow[]
}

// --- 変更履歴 ---

export type ChangeSet = {
  id: number
  created_at: string
  user: { id: number; name: string }
  scenario: { id: number; name: string } | null
  reason: string
  changes: number
  tables: Record<string, number>
  activities: { id: number; code: string; name: string }[]
  more_activities: number
}

export type ChangeLog = {
  id: number
  table_name: string
  record_id: number
  action: 'insert' | 'update' | 'delete'
  label: string
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
}

export const tableLabels: Record<string, string> = {
  activities: '施策',
  activity_external_codes: '外部コード',
  activity_milestones: 'マイルストーン',
  activity_drivers: 'ドライバー定義',
  activity_lines: '内訳',
  activity_formulas: '計算式', // 内訳の導入前の変更履歴用
  budget_facts: '金額',
  driver_values: 'ドライバー値',
  scenario_conditions: '想定条件',
  scenarios: 'シナリオ',
  units: 'ユニット',
  functions: 'ユニット', // 改称前（functions）の変更履歴用
  segments: 'セグメント',
  organizations: '組織',
  subjects: '科目',
  users: 'ユーザー',
}

// --- リスク ---

export type PL = { revenue: string; expense: string }

export type RiskActivity = {
  id: number
  code: string
  name: string
  unit_id: number
  owner_user_id: number | null
  activity_type: ActivityType
  status: ActivityStatus
  probability: number | null
  assumptions: string
  base: PL
  optimistic: PL | null
  pessimistic: PL | null
  provisional: { count: number; revenue: string; expense: string; reasons: string[] }
  milestones: { name: string; due_date: string; status: MilestoneStatus; risk: 'overdue' | 'delayed' | 'upcoming' }[]
  conditions: Partial<Record<'base' | 'optimistic' | 'pessimistic', string>>
}

export type RiskReport = {
  fiscal_year: number
  today: string
  base: { id: number; name: string; scenario_kind: ScenarioKind }
  optimistic: { id: number; name: string; scenario_kind: ScenarioKind } | null
  pessimistic: { id: number; name: string; scenario_kind: ScenarioKind } | null
  activities: RiskActivity[]
}
