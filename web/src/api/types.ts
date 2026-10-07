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
  /** 廃止（統合したユニットなど）。一覧・選択肢に出さない */
  is_archived: boolean
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
  /** Slack のメンバー ID（通知のメンション用）。空は未登録 */
  slack_user_id: string
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
  /** 確度の段階のコード（例: C） */
  confidence_level: string
  assumptions: string
  /** 重点施策（共有の印） */
  is_priority: boolean
  can_edit: boolean
  /** 施策の作成・削除・重点施策の設定ができるか */
  can_manage: boolean
  /** ログインユーザーがウォッチしているか（本人にだけ見える） */
  is_watched: boolean
}

/** 確度の段階（判定基準つき）。FP&A がマスタで管理する */
export type ConfidenceLevel = Timestamps & {
  id: number
  code: string
  name: string
  /** 標準の確率（0〜1） */
  rate: number | string
  criteria: string
  sort_order: number
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
/** 見通しの種類（docs/plan.md「2.8」） */
export type Outlook = 'base' | 'addon' | 'downside'

export const outlookLabels: Record<Outlook, string> = {
  base: 'ベース',
  addon: 'アドオン',
  downside: 'ダウンサイド',
}

export const outlookDescriptions: Record<Outlook, string> = {
  base: 'トレンドの延長。既存の契約・顧客から見込める分',
  addon: '新しい活動・追加要件による上積み',
  downside: '環境変化・不利な出来事による減少（金額はマイナスで入力）',
}

export type Line = Timestamps & {
  id: number
  activity_id: number
  subject_id: number
  name: string
  expression: string
  formula_enabled: boolean
  /** 内訳の確度の段階。null なら施策の段階 */
  confidence_level: string | null
  outlook: Outlook
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

/** シナリオのエイリアス。年度ごとに1つのシナリオにだけ付けられる */
export type PlanRole = 'initial' | 'revised' | 'latest'

export const planRoleLabels: Record<PlanRole, string> = {
  initial: '期初計画',
  revised: '修正計画',
  latest: '最新見込',
}

export type Scenario = Timestamps & {
  id: number
  name: string
  fiscal_year: number
  plan_role: PlanRole | null
  /** 決算確定月（YYYY-MM）。この月以前は実績、それより後は計画値 */
  actual_through: string | null
  /** 作成中（アプリ全体で1つ） */
  is_active: boolean
  /** 現場の更新の締切日（YYYY-MM-DD） */
  update_deadline: string | null
  base_scenario_id: number | null
  /** 前回見込（同じ年度のシナリオ）。比較やホームで「前回締めた見込」として使う */
  previous_scenario_id: number | null
  is_locked: boolean
  created_by: number
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
  source: 'manual' | 'formula' | 'import' | 'actual'
  is_provisional: boolean
  provisional_reason: string
}

/** 内訳の金額。formula_enabled なら計算式で算出され、直接入力できない */
export type AmountLine = {
  id: number
  name: string
  expression: string
  formula_enabled: boolean
  confidence_level: string | null
  outlook: Outlook
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
  confidence_level: string
  can_edit: boolean
}

/** 差異の要因の分類 */
export type NoteCause = 'timing' | 'volume' | 'new' | 'lost' | 'assumption' | 'other'

export const noteCauseLabels: Record<NoteCause, string> = {
  timing: '時期のずれ',
  volume: '数量・単価の増減',
  new: '新規',
  lost: '失注・解約',
  assumption: '前提の変化',
  other: 'その他',
}

/** 施策 × シナリオの状態 */
export type NoteStatus = 'not_started' | 'in_progress' | 'completed'

export const noteStatusLabels: Record<NoteStatus, string> = {
  not_started: '未着手',
  in_progress: '入力中',
  completed: '完了',
}

/** 施策 × シナリオの差異の説明と更新の状態 */
export type ActivityNote = {
  scenario_id: number
  activity_id: number
  explanation: string
  causes: NoteCause[]
  status: NoteStatus
  completed_at: string | null
  completed_by: number | null
  completed_by_name: string
  last_edited_at: string | null
}

/** GET /scenarios/{id}/activities/{aid} */
export type ValuesView = {
  scenario: Scenario
  activity: ActivitySummary
  months: string[]
  /** 実績の月（決算確定月以前）。入力できない */
  actual_months: string[]
  editable: boolean
  drivers: DriverRow[]
  amounts: AmountRow[]
  condition: string | null
  note: ActivityNote
}

export type ImportResult = {
  dry_run: boolean
  months: string[]
  /** CSV のデータ行数 */
  rows: number
  /** 対象外の会計科目の行数 */
  excluded: number
  /** 割当の根拠ごとの行数 */
  allocation: Record<AllocatedBy, number>
  facts: number
  inserted: number
  updated: number
  deleted: number
  unchanged: number
  totals: { month: string; revenue: string; expense: string; unallocated_revenue: string; unallocated_expense: string }[]
  /** 取り込むとロック済みのシナリオの実績と食い違う月（docs/plan.md「2.14」） */
  locked_drift: ScenarioDrift[]
}

// --- 実績の割当（docs/plan.md「2.12」） ---

export type GLAccount = {
  id: number
  code: string
  name: string
  /** アプリの科目。対象外なら null */
  subject_id: number | null
  is_excluded: boolean
  /** 明細を FP&A 以外に見せない */
  hide_details: boolean
}

export type AllocationRule = {
  id: number
  gl_account_id: number
  gl_account_code: string
  gl_account_name: string
  /** null は全部門 */
  department_code: string | null
  activity_id: number
  activity_code: string
  activity_name: string
}

export type AllocatedBy = 'activity_code' | 'external_code' | 'rule' | 'manual' | 'unallocated'

export const allocatedByLabels: Record<AllocatedBy, string> = {
  activity_code: '施策コード',
  external_code: '外部コード',
  rule: '割当ルール',
  manual: '未割当の一覧から選択',
  unallocated: '未割当',
}

export type UnallocatedGroup = {
  key: string
  box_code: string | null
  /** 箱の ID がないまとまりのみ */
  gl_account_id: number | null
  department_code: string | null
  accounts: string[]
  months: string[]
  count: number
  revenue: string
  expense: string
  description: string
}

export type UnallocatedList = {
  items: UnallocatedGroup[]
  count: number
  revenue: string
  expense: string
}

export type ActivityChange = {
  activity: { id: number; code: string; name: string } | null
  revenue: string
  expense: string
}

export type ReallocateResult = {
  dry_run: boolean
  months: string[]
  entries: number
  changed: number
  removed: number
  changes: ActivityChange[]
}

export type ActualEntry = {
  id: number
  target_month: string
  gl_account_code: string
  gl_account_name: string
  subject_id: number
  department_code: string | null
  box_code: string | null
  description: string
  amount: string
  allocated_by: AllocatedBy
}

export type ActualEntries = {
  month: string
  /** 施策の実績がある月 */
  months: string[]
  items: ActualEntry[]
  /** 明細を見せない会計科目の合計 */
  hidden: { gl_account_code: string; gl_account_name: string; subject_id: number; count: number; amount: string }[]
  entries_total: string
  fact_total: string
}

// --- 予実比較 ---

export type ReportSeries = {
  key: string
  label: string
  kind: 'scenario' | 'actual'
  scenario_id?: number
  /** シナリオの決算確定月（YYYY-MM） */
  actual_through?: string
}

export type ReportRow = {
  /** null は未割当の実績（ユニットを指定しないときだけ返る） */
  unit_id: number | null
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
  confidence_levels: '確度の段階',
  activity_scenario_notes: '差異の説明・更新の完了',
  users: 'ユーザー',
}

// --- リスク ---

export type PL = { revenue: string; expense: string }

export type RiskMilestone = { name: string; due_date: string; status: MilestoneStatus; risk: 'overdue' | 'delayed' | 'upcoming' }

/** リスクの警告（docs/plan.md「2.8」の客観的なシグナル） */
export type RiskWarnings = {
  milestones: RiskMilestone[]
  postponed: { count: number; days: number }
  downward: { diff: string; rate: number } | null
  consecutive: boolean
  accuracy: number | null
}

export type RiskLine = {
  /** null は科目への直接入力 */
  line_id: number | null
  subject_id: number
  name: string
  confidence_level: string
  outlook: Outlook
  amount: string
}

export type RiskActivity = {
  id: number
  code: string
  name: string
  unit_id: number
  owner_user_id: number | null
  activity_type: ActivityType
  status: ActivityStatus
  confidence_level: string
  assumptions: string
  /** 期間の満額・加重見込・楽観・悲観、うち実績 */
  full: PL
  weighted: PL
  optimistic: PL
  pessimistic: PL
  actual: PL
  /** 期間の売上（満額）を「actual・段階のコード・downside」に分けたもの */
  revenue_by_level: Record<string, string>
  /** 比較シナリオの期間の加重見込 */
  compare: PL | null
  lines: RiskLine[]
  warnings: RiskWarnings
  warning_count: number
  /** 段階に対して状況が悪い（確度の高い段階で警告あり） */
  bad_for_level: boolean
  conditions: Partial<Record<'scenario' | 'compare', string>>
}

export type RiskScenarioRef = { id: number; name: string; plan_role: PlanRole | null; actual_through: string | null; base_scenario_id: number | null }

export type RiskReport = {
  fiscal_year: number
  today: string
  period: 'year' | 'remaining'
  months: string[]
  scenario: RiskScenarioRef
  compare: RiskScenarioRef | null
  levels: { code: string; name: string; rate: string; high: boolean }[]
  activities: RiskActivity[]
}

// --- ホーム ---

export type PLTotals = { revenue: string; expense: string }

export type ScenarioRef = { id: number; name: string; plan_role: PlanRole | null; actual_through: string | null }

/** ホームの1行（施策 × シナリオの状態と、基準・前回見込との差） */
export type ActivityProgress = {
  activity_id: number
  code: string
  name: string
  unit_id: number
  owner_user_id: number | null
  status: NoteStatus
  has_explanation: boolean
  explanation: string
  causes: NoteCause[]
  is_priority: boolean
  is_watched: boolean
  last_edited_at: string | null
  completed_at: string | null
  current: PLTotals
  base: PLTotals | null
  initial: PLTotals | null
  revised: PLTotals | null
  previous: PLTotals | null
  /** 新しく実績になった月の、前回見込の計画値と実績の差 */
  accuracy: { plan: PLTotals; actual: PLTotals; rate: number | null; large: boolean } | null
}

/** GET /scenarios/{id}/activity-status */
export type ActivityProgressReport = {
  scenario: Scenario
  scope: 'mine' | 'units' | 'all'
  base: ScenarioRef | null
  initial: ScenarioRef | null
  revised: ScenarioRef | null
  previous: ScenarioRef | null
  new_actual_months: string[]
  items: ActivityProgress[]
}

// --- 締切と通知（docs/plan.md「2.13」） ---

export type NotificationKind = 'update_started' | 'deadline_reminder' | 'deadline_overdue' | 'actuals_reflected'

export const notificationKindLabels: Record<NotificationKind, string> = {
  update_started: '更新の開始',
  deadline_reminder: '締切の前',
  deadline_overdue: '締切の超過',
  actuals_reflected: '実績の反映',
}

export const notificationKindDescriptions: Record<NotificationKind, string> = {
  update_started: '作成中のシナリオに締切が入ったとき、対象の施策の担当者に1回',
  deadline_reminder: '締切の N 日前の送信時刻に、未完了の施策の担当者に',
  deadline_overdue: '締切の翌日から毎日、未完了の施策の担当者とユニットのマネージャーに（土日は送らない）',
  actuals_reflected: '決算確定月が進んだとき、前回見込との差が大きい施策の担当者に',
}

export type AppNotification = {
  id: number
  /** org_change_failed は組織変更の予約の失敗（FP&A 宛て） */
  kind: NotificationKind | 'org_change_failed'
  scenario_id: number | null
  title: string
  body: string
  link: string
  created_at: string
  read_at: string | null
}

export type NotificationList = { items: AppNotification[]; unread: number }

export type NotificationSettings = {
  enabled_kinds: Record<NotificationKind, boolean>
  reminder_days: number[]
  /** HH:MM（日本時間） */
  send_time: string
  slack_configured: boolean
}

export type NotificationRun = {
  id: number
  kind: NotificationKind
  scenario_id: number
  scenario_name: string
  run_date: string
  recipients: number
  slack_status: 'skipped' | 'sent' | 'failed'
  slack_attempts: number
  slack_error: string
  created_at: string
}

// --- 締めた後の実績の修正と年度の締め（docs/plan.md「2.14」） ---

/** 1か月の食い違い。差は「今の実績 − シナリオに保存した実績」。changed は施策 × 科目で金額が違う件数 */
export type DriftMonth = { month: string; revenue: string; expense: string; changed: number }

export type ScenarioDrift = {
  scenario_id: number
  name: string
  fiscal_year: number
  actual_through: string
  months: DriftMonth[]
}

export type RefreshActualsResult = { dry_run: boolean; drift: ScenarioDrift; saved: number }

export type FiscalYearClosing = { fiscal_year: number; closed_at: string; closed_by: number; closed_by_name: string }

// --- 組織変更と異動（docs/plan.md「2.15」） ---

export type OrgChangeKind = 'move_activity' | 'move_unit' | 'merge_unit' | 'change_owner'

export const orgChangeKindLabels: Record<OrgChangeKind, string> = {
  move_activity: '施策の移動',
  move_unit: 'ユニットの所属の変更',
  merge_unit: 'ユニットの統合',
  change_owner: '担当者の変更',
}

export type OrgChangeItem = {
  id?: number
  kind: OrgChangeKind
  activity_id: number | null
  unit_id: number | null
  target_unit_id: number | null
  segment_id: number | null
  organization_id: number | null
  /** 変更後の担当者・マネージャー（null は未設定） */
  owner_user_id: number | null
}

export type OrgChangeStatus = 'scheduled' | 'applied' | 'failed' | 'cancelled'

export const orgChangeStatusLabels: Record<OrgChangeStatus, string> = {
  scheduled: '予約中',
  applied: '適用済み',
  failed: '失敗',
  cancelled: '取り消し',
}

export type OrgChangePlan = {
  id: number
  name: string
  effective_date: string
  status: OrgChangeStatus
  created_by: number
  created_by_name: string
  applied_at: string | null
  error: string
  item_count: number
  items?: OrgChangeItem[]
}

export type Assignments = { activities: number; units: number }
