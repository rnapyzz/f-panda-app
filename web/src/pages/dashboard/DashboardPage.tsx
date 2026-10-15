import { useMemo, useState, type ReactNode } from 'react'
import { query } from '../../api/client'
import {
  activityTypeLabels,
  noteStatusLabels,
  planRoleLabels,
  unitTypeLabels,
  type Activity,
  type ActivityProgressReport,
  type ActivityType,
  type ComparisonReport,
  type List,
  type ReportRow,
  type RiskReport,
  type Scenario,
  type Subject,
  type TreeNode,
  type Unit,
  type UnitType,
  type User,
} from '../../api/types'
import { AmountUnitSwitch } from '../../components/AmountUnitSwitch'
import { ProfitBand } from '../../components/ProfitBand'
import { RestrictedNote } from '../../components/RestrictedNote'
import { Badge, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../../components/ui'
import { varianceRate } from '../../lib/aggregate'
import { AmountUnitProvider, useAmountUnit } from '../../lib/amountUnit'
import { useActiveScenario } from '../../lib/activeScenario'
import { useCurrentUser } from '../../lib/auth'
import {
  breadcrumb,
  causeKeyOf,
  causeLabels,
  childGroups,
  filtersQuery,
  groupIncludes,
  parseFilters,
  profitOf,
  scopeLabels,
  scopeOf,
  scopedRows,
  totalsByActivity,
  totalsBySeries,
  varianceBars,
  waterfall,
  type DashActivity,
  type DashFilters,
  type DashScope,
  type Group,
  type PL,
  type Step,
  type VarianceBar,
  type WaterfallEntry,
} from '../../lib/dashboard'
import { formatRate, type AmountUnit } from '../../lib/format'
import { countByStatus } from '../../lib/home'
import { compositionKeys, sumComposition, sumMeasures, profitOf as riskProfit } from '../../lib/risk'
import { Link, navigate, useLocation } from '../../lib/router'
import { currentFiscalYear, defaultScenarios, fiscalMonths, scenarioLabel } from '../../lib/scenario'
import { buildTree, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'
import { useActivityRows } from '../../lib/useActivityRows'
import { CompositionBar, ProgressBar, TrendChart, VarianceChart, WaterfallCard } from './DashboardCharts'

/**
 * ダッシュボード（docs/plan.md「2.30」）。見込の状況を図で見て、図を押して掘り下げる。
 * 絞り込み・掘り下げの状態は URL のクエリに持つ（lib/dashboard.ts）。
 */
export function DashboardPage() {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const units = useApi<List<Unit>>('/units?include_archived=true')
  const segments = useApi<List<TreeNode>>('/segments')
  const subjects = useApi<List<Subject>>('/subjects')
  const activities = useApi<List<Activity>>('/activities')
  const users = useApi<List<User>>('/users')
  const error = scenarios.error ?? units.error ?? segments.error ?? subjects.error ?? activities.error ?? users.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !units.data || !segments.data || !subjects.data || !activities.data || !users.data) return <Loading />
  return (
    <Dashboard
      scenarios={scenarios.data.items}
      units={units.data.items}
      segments={segments.data.items}
      subjects={subjects.data.items}
      activities={activities.data.items}
      users={users.data.items}
    />
  )
}

/** ロールごとの初めの範囲（docs/plan.md「2.30」） */
function defaultScope(role: string): DashScope {
  if (role === 'member') return 'mine'
  if (role === 'manager') return 'units'
  return 'all'
}

function Dashboard({
  scenarios,
  units,
  segments,
  subjects,
  activities,
  users,
}: {
  scenarios: Scenario[]
  units: Unit[]
  segments: TreeNode[]
  subjects: Subject[]
  activities: Activity[]
  users: User[]
}) {
  const me = useCurrentUser()
  const { active } = useActiveScenario()
  const { search } = useLocation()
  const defaults = { scope: defaultScope(me.role) }
  const f = parseFilters(search, defaults)
  const [amountUnit, setAmountUnit] = useState<AmountUnit>('million')
  const [waterfallMode, setWaterfallMode] = useState<'group' | 'cause'>('group')
  const set = (patch: Partial<DashFilters>) => navigate(`/dashboard${filtersQuery({ ...f, ...patch }, defaults)}`)

  // 年度・シナリオ・比較
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const fy = f.fy ?? (active && years.includes(active.fiscal_year) ? active.fiscal_year : years.includes(currentFiscalYear()) ? currentFiscalYear() : years[0])
  const inYear = scenarios.filter((s) => s.fiscal_year === fy)
  const scenario =
    inYear.find((s) => s.id === f.scenario) ?? inYear.find((s) => s.id === active?.id) ?? defaultScenarios(inYear).latest ?? inYear[0]
  const target = defaultScenarios(inYear).base
  const previous = scenario?.previous_scenario_id ? inYear.find((s) => s.id === scenario.previous_scenario_id) : undefined
  const compare = f.compare === 'previous' ? previous : target
  const compareScenario = compare && compare.id !== scenario?.id ? compare : undefined
  const previousScenario = previous && previous.id !== scenario?.id ? previous : undefined
  // 系列の名前: 比較の目標（修正計画、なければ期初計画）はエイリアス、ほかはシナリオ名で出す（「目標」「今回」だけだと、どのシナリオか分からないため）
  const nameOf = (s: Scenario | undefined, fallback: string) => (s ? s.name : fallback)
  const aliasOrName = (s: Scenario | undefined, fallback: string) => (s ? (s.plan_role ? planRoleLabels[s.plan_role] : s.name) : fallback)
  const labels = {
    current: scenario ? nameOf(scenario, '') : '',
    previous: nameOf(previousScenario, '前回の見込'),
    compare: f.compare === 'previous' ? nameOf(previousScenario, '前回の見込') : aliasOrName(compareScenario, '目標'),
  }
  const compareLabel = labels.compare

  const ids = scenario ? [...new Set([scenario.id, compareScenario?.id, previousScenario?.id].filter((x): x is number => x !== undefined))] : []
  const reportQuery = ids.length > 0 ? query({ scenario_ids: ids.join(',') }) : null
  const unitReport = useApi<ComparisonReport>(reportQuery ? `/reports/comparison${reportQuery}` : null)
  const status = useApi<ActivityProgressReport>(scenario ? `/scenarios/${scenario.id}/activity-status?scope=all` : null)
  const risk = useApi<RiskReport>(scenario ? `/reports/risk${query({ scenario_id: scenario.id })}` : null)

  const tree = useMemo(() => buildTree(segments), [segments])
  const statusById = useMemo(() => new Map((status.data?.items ?? []).map((it) => [it.activity_id, it])), [status.data])
  const dashActivities: DashActivity[] = useMemo(
    () =>
      activities.map((a) => ({
        id: a.id,
        code: a.code,
        name: a.name,
        unit_id: a.unit_id,
        owner_user_id: a.owner_user_id,
        activity_type: a.activity_type,
        confidence_level: a.confidence_level,
        causes: statusById.get(a.id)?.causes ?? [],
      })),
    [activities, statusById],
  )
  const scope = scopeOf({ ...f, fy }, tree, units, dashActivities, me)
  const scopeUnitIds = [...scope.units].sort((a, b) => a - b)
  const activityRows = useActivityRows(reportQuery, scopeUnitIds)

  const errorAll = unitReport.error ?? status.error ?? risk.error ?? activityRows.error
  const header = (
    <PageHeader
      title="ダッシュボード"
      description="見込の状況を図で見られます。図の棒や区分を押すと、画面全体がそこに絞り込まれます。"
      actions={<AmountUnitSwitch value={amountUnit} onChange={setAmountUnit} />}
    />
  )
  if (!scenario) {
    return (
      <>
        {header}
        <Card>
          <Empty>シナリオがまだありません。</Empty>
        </Card>
      </>
    )
  }

  const months = fiscalMonths(fy)
  const keyOf = (id: number | undefined) => unitReport.data?.series.find((s) => s.scenario_id === id)?.key
  const keys = { current: keyOf(scenario.id), compare: keyOf(compareScenario?.id), previous: keyOf(previousScenario?.id) }
  const subjectById = new Map(subjects.map((s) => [s.id, s]))
  const categoryOf = (id: number) => subjectById.get(id)?.category
  const ready = unitReport.data && activityRows.rows && status.data && risk.data && keys.current
  const rows = ready ? scopedRows(unitReport.data!.rows, activityRows.rows!, scope) : []
  const seriesKeys = [keys.current, keys.compare, keys.previous].filter((k): k is string => !!k)
  const totals = ready ? totalsBySeries(rows, seriesKeys, categoryOf, months) : null
  const userName = new Map(users.map((u) => [u.id, u.name]))
  const unitName = new Map(units.map((u) => [u.id, u.name]))

  const onGroup = (g: Group) => {
    if (g.kind === 'segment') set({ segment: g.id, unit: null })
    else if (g.kind === 'unit') set({ unit: g.id })
    else if (g.kind === 'activity') navigate(`/activities/${g.id}`)
  }

  return (
    <AmountUnitProvider unit={amountUnit}>
      {header}
      <FilterBar
        f={{ ...f, fy }}
        years={years}
        inYear={inYear}
        scenario={scenario}
        target={target && target.id !== scenario.id ? target : undefined}
        previous={previousScenario}
        set={set}
        role={me.role}
      />
      <Crumbs f={f} tree={tree} units={units} set={set} />
      {errorAll ? (
        <ErrorMessage error={errorAll} />
      ) : !ready || !totals ? (
        <Loading />
      ) : (
        <div className="space-y-4">
          <RestrictedNote hidden={unitReport.data!.restricted_hidden} />
          {scope.activities.size === 0 && !scope.withUnallocated ? (
            <Card>
              <Empty>この条件に合う施策はありません。絞り込みを広げてください。</Empty>
            </Card>
          ) : (
            <>
              <KpiTiles
                current={totals.get(keys.current!)!}
                compare={keys.compare ? totals.get(keys.compare)!.year : null}
                previous={keys.previous ? totals.get(keys.previous)!.year : null}
                compareLabel={compareLabel}
                previousLabel={labels.previous}
                showPrevious={f.compare !== 'previous'}
                actualThrough={scenario.actual_through}
                scenario={scenario}
              />
              <div className="grid gap-4 xl:grid-cols-2">
                <Section title="月次の推移" sub={scenarioLabel(scenario)}>
                  <TrendChart
                    months={months}
                    actualThrough={scenario.actual_through}
                    series={{
                      current: totals.get(keys.current!)!.byMonth,
                      compare: keys.compare ? totals.get(keys.compare)!.byMonth : null,
                      previous: keys.previous && keys.previous !== keys.compare ? totals.get(keys.previous)!.byMonth : null,
                    }}
                    measure={f.measure}
                    onMeasure={(m) => set({ measure: m })}
                    labels={labels}
                  />
                </Section>
                <VarianceSection f={f} tree={tree} units={units} activities={dashActivities} scope={scope} rows={rows} keys={keys} categoryOf={categoryOf} months={months} compareLabel={compareLabel} currentLabel={labels.current} onGroup={onGroup} />
                <ChangesSection
                  f={f}
                  tree={tree}
                  units={units}
                  activities={dashActivities}
                  scope={scope}
                  rows={rows}
                  keys={keys}
                  categoryOf={categoryOf}
                  months={months}
                  mode={waterfallMode}
                  onMode={setWaterfallMode}
                  onGroup={onGroup}
                  onCause={(c) => set({ cause: f.cause === c ? null : c })}
                  statusById={statusById}
                  labels={labels}
                />
                <CertaintySection risk={risk.data!} status={status.data!} scope={scope} f={f} onLevel={(code) => set({ level: f.level === code ? null : code })} />
              </div>
              <ActivitiesSection
                rows={rows}
                keys={keys}
                categoryOf={categoryOf}
                months={months}
                activities={dashActivities}
                scope={scope}
                compareLabel={compareLabel}
                statusById={statusById}
                unitName={(id) => unitName.get(id) ?? ''}
                userName={(id) => (id ? (userName.get(id) ?? '') : '')}
                levelName={(code) => {
                  const l = risk.data!.levels.find((x) => x.code === code)
                  return l ? `${l.code} ${l.name}` : code
                }}
              />
            </>
          )}
        </div>
      )}
    </AmountUnitProvider>
  )
}

function Section({ title, sub, children }: { title: string; sub?: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="min-w-0">
      <h2 className="mb-2 flex flex-wrap items-baseline gap-x-2 text-sm font-semibold text-slate-800">
        {title}
        {sub && <span className="text-xs font-normal text-slate-500">{sub}</span>}
      </h2>
      {children}
    </section>
  )
}

// --- 絞り込み ---

function FilterBar({
  f,
  years,
  inYear,
  scenario,
  target,
  previous,
  set,
  role,
}: {
  f: DashFilters & { fy: number }
  years: number[]
  inYear: Scenario[]
  scenario: Scenario
  /** 比較の目標（修正計画、なければ期初計画）と前回の見込のシナリオ */
  target: Scenario | undefined
  previous: Scenario | undefined
  set: (patch: Partial<DashFilters>) => void
  role: string
}) {
  const scopes: DashScope[] = role === 'member' ? ['mine', 'all'] : role === 'manager' ? ['units', 'mine', 'all'] : ['all', 'units', 'mine']
  return (
    <Card className="mb-3">
      <div className="flex flex-wrap items-end gap-3 text-xs">
        <Control label="年度">
          <Select aria-label="年度" value={f.fy} onChange={(e) => set({ fy: Number(e.target.value), scenario: null, segment: null, unit: null })} className="w-28 py-1 text-xs">
            {years.map((y) => (
              <option key={y} value={y}>
                {y}年度
              </option>
            ))}
          </Select>
        </Control>
        <Control label="今回">
          <Select aria-label="今回のシナリオ" value={scenario.id} onChange={(e) => set({ scenario: Number(e.target.value) })} className="w-56 py-1 text-xs">
            {inYear.map((s) => (
              <option key={s.id} value={s.id}>
                {scenarioLabel(s)}
              </option>
            ))}
          </Select>
        </Control>
        <Link to={`/scenarios/${scenario.id}`} className="pb-1.5 text-xs font-medium text-indigo-700 hover:underline">
          このシナリオの全体像 →
        </Link>
        <Control label="比較">
          <Select aria-label="比較" value={f.compare} onChange={(e) => set({ compare: e.target.value as DashFilters['compare'] })} className="w-64 py-1 text-xs">
            <option value="target" disabled={!target}>
              {target ? scenarioLabel(target) : '目標（修正計画・期初計画）なし'}
            </option>
            <option value="previous" disabled={!previous}>
              {previous ? `${previous.name}（前回の見込）` : '前回の見込なし'}
            </option>
          </Select>
        </Control>
        <Control label="範囲">
          <Select aria-label="範囲" value={f.scope} onChange={(e) => set({ scope: e.target.value as DashScope })} className="w-40 py-1 text-xs">
            {scopes.map((s) => (
              <option key={s} value={s}>
                {scopeLabels[s]}
              </option>
            ))}
          </Select>
        </Control>
        <Control label="施策のタイプ">
          <Select aria-label="施策のタイプ" value={f.type} onChange={(e) => set({ type: e.target.value as ActivityType | '' })} className="w-36 py-1 text-xs">
            <option value="">すべて</option>
            {Object.entries(activityTypeLabels).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </Select>
        </Control>
        <Control label="ユニットの種別">
          <Select aria-label="ユニットの種別" value={f.unitType} onChange={(e) => set({ unitType: e.target.value as UnitType | '' })} className="w-32 py-1 text-xs">
            <option value="">すべて</option>
            {Object.entries(unitTypeLabels).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </Select>
        </Control>
      </div>
    </Card>
  )
}

function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="space-y-1">
      <span className="block font-medium text-slate-500">{label}</span>
      {children}
    </label>
  )
}

/** パンくずと、図から選んだ絞り込み（要因の分類・確度の段階）。押すと戻る・外す */
function Crumbs({ f, tree, units, set }: { f: DashFilters; tree: Tree; units: Unit[]; set: (patch: Partial<DashFilters>) => void }) {
  const crumbs = breadcrumb(f, tree, units)
  return (
    <nav aria-label="掘り下げの位置" className="mb-4 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
      {crumbs.map((c, i) => {
        const last = i === crumbs.length - 1
        return (
          <span key={`${c.segment}-${c.unit}`} className="flex items-center gap-1.5">
            {i > 0 && <span className="text-slate-400">›</span>}
            {last ? (
              <span aria-current="page" className="font-semibold text-slate-900">
                {c.label}
              </span>
            ) : (
              <button type="button" className="text-indigo-700 hover:underline" onClick={() => set({ segment: c.segment, unit: c.unit })}>
                {c.label}
              </button>
            )}
          </span>
        )
      })}
      {f.cause && <Chip label={`要因: ${causeLabels[f.cause]}`} onRemove={() => set({ cause: null })} />}
      {f.level && <Chip label={`確度: ${f.level}`} onRemove={() => set({ level: null })} />}
    </nav>
  )
}

function Chip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="ml-2 inline-flex items-center gap-1 rounded-full bg-indigo-50 py-0.5 pr-1 pl-2.5 text-xs text-indigo-800">
      {label}
      <button type="button" aria-label={`${label} の絞り込みを外す`} onClick={onRemove} className="rounded-full px-1 text-indigo-500 hover:bg-indigo-100 hover:text-indigo-800">
        ×
      </button>
    </span>
  )
}

// --- ① 年間の状況 ---

function KpiTiles({
  current,
  compare,
  previous,
  compareLabel,
  previousLabel,
  showPrevious,
  actualThrough,
  scenario,
}: {
  current: { year: PL; byMonth: Map<string, PL> }
  compare: PL | null
  previous: PL | null
  compareLabel: string
  previousLabel: string
  /** 比較とは別に、前回の見込との差も出すか（比較が前回の見込なら出さない） */
  showPrevious: boolean
  actualThrough: string | null
  scenario: Scenario
}) {
  const { fmt, signed } = useAmountUnit()
  const actualProfit = [...current.byMonth.entries()].filter(([m]) => actualThrough !== null && m <= actualThrough).reduce((s, [, v]) => s + profitOf(v), 0n)
  const yearProfit = profitOf(current.year)
  const progress = actualThrough && yearProfit !== 0n ? Number((actualProfit * 1000n) / yearProfit) / 10 : null
  const tile = (label: string, measure: 'revenue' | 'expense' | 'profit') => {
    const pick = (p: PL) => (measure === 'profit' ? profitOf(p) : p[measure])
    const v = pick(current.year)
    const diffs: { label: string; diff: bigint; rate: number | null }[] = []
    if (compare) diffs.push({ label: compareLabel, diff: v - pick(compare), rate: varianceRate(pick(compare), v) })
    if (previous && showPrevious) diffs.push({ label: previousLabel, diff: v - pick(previous), rate: varianceRate(pick(previous), v) })
    const good = (d: bigint) => (d === 0n ? null : measure === 'expense' ? d < 0n : d > 0n)
    return (
      <div key={measure} className={cx('rounded-lg border bg-white px-4 py-3', measure === 'profit' ? 'border-indigo-200' : 'border-slate-200')}>
        <div className="text-xs font-medium text-slate-500">{label}（年間）</div>
        <div className="mt-0.5 text-2xl font-semibold tracking-tight text-slate-900 tabular-nums">{fmt(v)}</div>
        <dl className="mt-1 space-y-0.5 text-xs">
          {diffs.map((d) => (
            <div key={d.label} className="flex items-baseline justify-between gap-2">
              <dt className="text-slate-500">{d.label}との差</dt>
              <dd className={cx('tabular-nums', good(d.diff) === true && 'text-emerald-700', good(d.diff) === false && 'text-red-600', good(d.diff) === null && 'text-slate-500')}>
                {signed(d.diff)}
                {d.rate !== null && <span className="ml-1 text-slate-500">({formatRate(d.rate)})</span>}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    )
  }
  return (
    <section aria-label="年間の状況" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      {tile('売上', 'revenue')}
      {tile('費用', 'expense')}
      {tile('利益', 'profit')}
      <div className="rounded-lg border border-slate-200 bg-white px-4 py-3">
        <div className="text-xs font-medium text-slate-500">実績の進み（利益）</div>
        <div className="mt-0.5 text-2xl font-semibold tracking-tight text-slate-900 tabular-nums">{progress === null ? '—' : `${progress.toFixed(1)}%`}</div>
        <div className="mt-1 text-xs text-slate-500">
          {actualThrough ? `${Number(actualThrough.slice(5, 7))}月までの実績 ${fmt(actualProfit)} ÷ 年間の見込` : '実績の月はまだありません'}
        </div>
        {progress !== null && (
          <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-slate-100">
            <div className="h-full rounded-full bg-indigo-500" style={{ width: `${Math.max(0, Math.min(100, progress))}%` }} />
          </div>
        )}
        <div className="mt-1 truncate text-[11px] text-slate-400">{scenarioLabel(scenario)}</div>
      </div>
    </section>
  )
}

// --- ③ どこで差が出ているか ---

type Keys = { current?: string; compare?: string; previous?: string }

function VarianceSection({
  f,
  tree,
  units,
  activities,
  scope,
  rows,
  keys,
  categoryOf,
  months,
  compareLabel,
  currentLabel,
  onGroup,
}: {
  f: DashFilters
  tree: Tree
  units: Unit[]
  activities: DashActivity[]
  scope: ReturnType<typeof scopeOf>
  rows: ReportRow[]
  keys: Keys
  categoryOf: (id: number) => Subject['category'] | undefined
  months: string[]
  compareLabel: string
  currentLabel: string
  onGroup: (g: Group) => void
}) {
  if (!keys.compare) {
    return (
      <Section title="どこで差が出ているか">
        <Card>
          <Empty>比較するシナリオ（{compareLabel}）がありません。</Empty>
        </Card>
      </Section>
    )
  }
  const groups = childGroups(f, tree, units, activities, scope)
  const bars = varianceBars(groups, rows, (g) => groupIncludes(g, tree, units), { current: keys.current!, compare: keys.compare }, categoryOf, months)
  const level = f.unit !== null ? '施策ごと' : groups.some((g) => g.kind === 'segment') ? 'セグメントごと' : 'ユニットごと'
  return (
    <Section title="どこで差が出ているか" sub={`${level}・${compareLabel}との差（利益）`}>
      {bars.length === 0 ? (
        <Card>
          <Empty>金額のある区分がありません。</Empty>
        </Card>
      ) : (
        <VarianceChart bars={bars} compareLabel={compareLabel} currentLabel={currentLabel} onSelect={(b: VarianceBar) => onGroup(b.group)} />
      )}
    </Section>
  )
}

// --- ④ 前回から何が変わったか ---

function ChangesSection({
  f,
  tree,
  units,
  activities,
  scope,
  rows,
  keys,
  categoryOf,
  months,
  mode,
  onMode,
  onGroup,
  onCause,
  statusById,
  labels,
}: {
  f: DashFilters
  tree: Tree
  units: Unit[]
  activities: DashActivity[]
  scope: ReturnType<typeof scopeOf>
  rows: ReportRow[]
  keys: Keys
  categoryOf: (id: number) => Subject['category'] | undefined
  months: string[]
  mode: 'group' | 'cause'
  onMode: (m: 'group' | 'cause') => void
  onGroup: (g: Group) => void
  onCause: (c: ReturnType<typeof causeKeyOf>) => void
  statusById: Map<number, ActivityProgressReport['items'][number]>
  labels: { current: string; previous: string }
}) {
  const { signed } = useAmountUnit()
  if (!keys.previous) {
    return (
      <Section title="前回から何が変わったか">
        <Card>
          <Empty>前回の見込がありません（シナリオ管理で前回見込を指定すると出ます）。</Empty>
        </Card>
      </Section>
    )
  }
  const byActivity = totalsByActivity(rows, [keys.current!, keys.previous], categoryOf, months)
  const actById = new Map(activities.map((a) => [a.id, a]))
  const groups = childGroups(f, tree, units, activities, scope)
  const groupOfActivity = (a: DashActivity): Group | undefined =>
    groups.find((g) => (g.kind === 'activity' ? g.id === a.id : g.kind === 'unit' ? g.id === a.unit_id : g.kind === 'segment' ? groupIncludes(g, tree, units)({ unit_id: a.unit_id } as ReportRow) : false))
  const entries: WaterfallEntry[] = [...byActivity.entries()].map(([id, bySeries]) => {
    const current = profitOf(bySeries.get(keys.current!)!)
    const previous = profitOf(bySeries.get(keys.previous!)!)
    const a = actById.get(id)
    if (!a) return { key: 'unallocated', label: '未割当', filter: null, current, previous }
    if (mode === 'cause') {
      const c = causeKeyOf(a)
      return { key: `c:${c}`, label: causeLabels[c], filter: { kind: 'cause', cause: c }, current, previous }
    }
    const g = groupOfActivity(a)
    return g ? { key: g.key, label: g.name, filter: g.kind === 'unallocated' ? null : { kind: 'group', group: g }, current, previous } : { key: 'other', label: 'その他', filter: null, current, previous }
  })
  const steps = waterfall(entries, 8, { start: labels.previous, end: labels.current })
  const onSelect = (s: Step) => {
    if (s.filter?.kind === 'group') onGroup(s.filter.group)
    else if (s.filter?.kind === 'cause') onCause(s.filter.cause)
  }
  // 増減の大きい施策と、その説明
  const changed = [...byActivity.entries()]
    .filter(([id]) => actById.has(id))
    .map(([id, s]) => ({ a: actById.get(id)!, diff: profitOf(s.get(keys.current!)!) - profitOf(s.get(keys.previous!)!) }))
    .filter((x) => x.diff !== 0n)
    .sort((x, y) => (x.diff < 0n ? -x.diff : x.diff) > (y.diff < 0n ? -y.diff : y.diff) ? -1 : 1)
    .slice(0, 3)
  return (
    <Section title="前回から何が変わったか" sub={`前回の見込「${labels.previous}」→「${labels.current}」（利益）`}>
      <WaterfallCard
        steps={steps}
        mode={mode}
        onMode={onMode}
        onSelect={onSelect}
        footer={mode === 'cause' ? '要因の分類は、今回の見込の説明で選んだものです。区分を押すと、その要因の施策に絞り込みます。' : '区分を押すと、そこに掘り下げます。'}
      />
      {changed.length > 0 && (
        <ul className="mt-2 space-y-1.5" aria-label="増減の大きい施策">
          {changed.map(({ a, diff }) => {
            const st = statusById.get(a.id)
            return (
              <li key={a.id} className="rounded-md border border-slate-200 bg-white px-3 py-2 text-xs">
                <div className="flex items-baseline justify-between gap-2">
                  <Link to={`/activities/${a.id}`} className="font-medium text-indigo-700 hover:underline">
                    {a.name}
                  </Link>
                  <span className={cx('tabular-nums', diff > 0n ? 'text-emerald-700' : 'text-red-600')}>{signed(diff)}</span>
                </div>
                <p className="mt-0.5 line-clamp-2 text-slate-600">{st?.explanation || <span className="text-slate-400">説明はまだありません</span>}</p>
              </li>
            )
          })}
        </ul>
      )}
    </Section>
  )
}

// --- ⑤ どれくらい確かか ---

function CertaintySection({
  risk,
  status,
  scope,
  f,
  onLevel,
}: {
  risk: RiskReport
  status: ActivityProgressReport
  scope: ReturnType<typeof scopeOf>
  f: DashFilters
  onLevel: (code: string) => void
}) {
  const items = risk.activities.filter((a) => scope.activities.has(a.id))
  const t = sumMeasures(items)
  const keys = compositionKeys(risk.levels)
  const composition = sumComposition(items, keys)
  const counts = countByStatus(status.items.filter((it) => scope.activities.has(it.activity_id)))
  return (
    <Section title="どれくらい確かか" sub="利益の振れ幅と、売上の確度の構成">
      <Card>
        <ProfitBand pessimistic={riskProfit(t.pessimistic)} weighted={riskProfit(t.weighted)} optimistic={riskProfit(t.optimistic)} actual={riskProfit(t.actual)} />
        <div className="mt-2 border-t border-slate-100 pt-3">
          <h3 className="mb-1.5 text-xs font-semibold text-slate-500">売上の確度の構成（段階を押すと絞り込む）</h3>
          <CompositionBar composition={composition} levels={risk.levels} selected={f.level} onSelect={onLevel} />
        </div>
        <div className="mt-3 border-t border-slate-100 pt-3">
          <h3 className="mb-1.5 text-xs font-semibold text-slate-500">更新の進み具合</h3>
          <ProgressBar counts={counts} />
        </div>
      </Card>
    </Section>
  )
}

// --- ⑥ 施策 ---

function ActivitiesSection({
  rows,
  keys,
  categoryOf,
  months,
  activities,
  scope,
  compareLabel,
  statusById,
  unitName,
  userName,
  levelName,
}: {
  rows: ReportRow[]
  keys: Keys
  categoryOf: (id: number) => Subject['category'] | undefined
  months: string[]
  activities: DashActivity[]
  scope: ReturnType<typeof scopeOf>
  compareLabel: string
  statusById: Map<number, ActivityProgressReport['items'][number]>
  unitName: (id: number) => string
  userName: (id: number | null) => string
  levelName: (code: string) => string
}) {
  const { fmt, signed } = useAmountUnit()
  const [all, setAll] = useState(false)
  const seriesKeys = [keys.current!, keys.compare].filter((k): k is string => !!k)
  const byActivity = totalsByActivity(rows, seriesKeys, categoryOf, months)
  const list = activities
    .filter((a) => scope.activities.has(a.id))
    .map((a) => {
      const s = byActivity.get(a.id)
      const current = s ? profitOf(s.get(keys.current!)!) : 0n
      const compare = s && keys.compare ? profitOf(s.get(keys.compare)!) : null
      return { a, current, diff: compare === null ? null : current - compare }
    })
    .filter((x) => x.current !== 0n || (x.diff ?? 0n) !== 0n)
    .sort((x, y) => {
      const ax = x.diff === null ? 0n : x.diff < 0n ? -x.diff : x.diff
      const ay = y.diff === null ? 0n : y.diff < 0n ? -y.diff : y.diff
      return ax === ay ? x.a.code.localeCompare(y.a.code) : ax > ay ? -1 : 1
    })
  const shown = all ? list : list.slice(0, 15)
  return (
    <Section title="施策" sub={`いまの絞り込みの施策（${list.length}件）・${compareLabel}との差の大きい順`}>
      <Card>
        {list.length === 0 ? (
          <Empty>金額のある施策はありません。</Empty>
        ) : (
          <>
            <div className="overflow-x-auto">
              <Table>
                <thead>
                  <tr>
                    <th>施策</th>
                    <th className="hidden md:table-cell">ユニット</th>
                    <th className="hidden md:table-cell">担当者</th>
                    <th className="text-right whitespace-nowrap">今回の利益</th>
                    <th className="text-right whitespace-nowrap">{compareLabel}との差</th>
                    <th className="hidden sm:table-cell">確度</th>
                    <th>状態</th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map(({ a, current, diff }) => {
                    const st = statusById.get(a.id)
                    return (
                      <tr key={a.id}>
                        <td>
                          <Link to={`/activities/${a.id}`} className="text-indigo-700 hover:underline">
                            <span className="mr-1 font-mono text-xs text-slate-500">{a.code}</span>
                            {a.name}
                          </Link>
                        </td>
                        <td className="hidden text-slate-600 md:table-cell">{unitName(a.unit_id)}</td>
                        <td className="hidden whitespace-nowrap text-slate-600 md:table-cell">{userName(a.owner_user_id)}</td>
                        <td className="text-right whitespace-nowrap tabular-nums">{fmt(current)}</td>
                        <td className={cx('text-right whitespace-nowrap tabular-nums', diff !== null && diff > 0n && 'text-emerald-700', diff !== null && diff < 0n && 'text-red-600')}>{diff === null ? '—' : signed(diff)}</td>
                        <td className="hidden text-xs whitespace-nowrap sm:table-cell">{a.confidence_level ? levelName(a.confidence_level) : ''}</td>
                        <td className="whitespace-nowrap">{st ? <Badge tone={st.status === 'completed' ? 'green' : st.status === 'in_progress' ? 'amber' : 'slate'}>{noteStatusLabels[st.status]}</Badge> : <span className="text-xs text-slate-400">対象外</span>}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </Table>
            </div>
            {list.length > shown.length && (
              <button type="button" className="mt-2 text-xs text-indigo-700 hover:underline" onClick={() => setAll(true)}>
                すべて表示（ほか {list.length - shown.length} 件）
              </button>
            )}
          </>
        )}
      </Card>
    </Section>
  )
}
