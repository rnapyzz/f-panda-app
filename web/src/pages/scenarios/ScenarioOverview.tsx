import { useMemo, useState, type ReactNode } from 'react'
import { query } from '../../api/client'
import {
  activityTypeLabels,
  outlookLabels,
  type Activity,
  type ActivityProgressReport,
  type ComparisonReport,
  type List,
  type RiskReport,
  type Scenario,
  type Subject,
  type TreeNode,
  type Unit,
} from '../../api/types'
import { AmountUnitSwitch } from '../../components/AmountUnitSwitch'
import { RestrictedNote } from '../../components/RestrictedNote'
import { Card, Empty, ErrorMessage, Loading, Table, cx } from '../../components/ui'
import { AmountUnitProvider, useAmountUnit } from '../../lib/amountUnit'
import { useCurrentUser } from '../../lib/auth'
import { typeColor } from '../../lib/chartTheme'
import { breadcrumb, childGroups, groupIncludes, parseFilters, profitOf, scopeOf, scopedRows, totalsBySeries, type DashActivity, type DashFilters, type Group } from '../../lib/dashboard'
import { formatRate, type AmountUnit } from '../../lib/format'
import {
  growthHints,
  groupTotals,
  halves,
  newVsContinuing,
  plStatement,
  revenueByActivity,
  revenueByType,
  riskHints,
  shareRate,
  summarySentences,
  topRate,
  topShares,
  typeOrder,
  type GrowthHints,
  type RiskHints,
} from '../../lib/overview'
import { compositionKeys, highCertaintyRate, sumComposition, sumMeasures, type Totals } from '../../lib/risk'
import { Link, navigate, useLocation } from '../../lib/router'
import { fiscalMonths, scenarioLabel } from '../../lib/scenario'
import { buildTree, type Tree } from '../../lib/tree'
import { useActivityRows } from '../../lib/useActivityRows'
import { useApi } from '../../lib/useApi'
import type { Measure } from '../../lib/aggregate'
import { TimelineChart } from '../activities/ActivityCharts'
import { CompositionBar, TrendChart } from '../dashboard/DashboardCharts'
import { AmountBars, ShareStack, Stat } from './OverviewCharts'
import { ScenarioNarrative } from './ScenarioNarrative'

/**
 * シナリオの全体像（docs/plan.md「2.31」）。比較をせずに、1つのシナリオの金額の大きさと構成を見せる。
 * 掘り下げの位置（segment・unit）は URL のクエリに持つ（tab=overview と一緒に）。
 */
export function ScenarioOverview({ scenario }: { scenario: Scenario }) {
  const units = useApi<List<Unit>>('/units?include_archived=true')
  const segments = useApi<List<TreeNode>>('/segments')
  const subjects = useApi<List<Subject>>('/subjects')
  const activities = useApi<List<Activity>>('/activities')
  const error = units.error ?? segments.error ?? subjects.error ?? activities.error
  if (error) return <ErrorMessage error={error} />
  if (!units.data || !segments.data || !subjects.data || !activities.data) return <Loading />
  return <Overview scenario={scenario} units={units.data.items} segments={segments.data.items} subjects={subjects.data.items} activities={activities.data.items} />
}

function Overview({ scenario, units, segments, subjects, activities }: { scenario: Scenario; units: Unit[]; segments: TreeNode[]; subjects: Subject[]; activities: Activity[] }) {
  const me = useCurrentUser()
  const { search } = useLocation()
  const [amountUnit, setAmountUnit] = useState<AmountUnit>('million')
  const [measure, setMeasure] = useState<Measure>('revenue')
  // 範囲は全社から。掘り下げの位置だけを URL から読む
  const pos = parseFilters(search, { scope: 'all' })
  const f: DashFilters = { ...pos, scope: 'all', type: '', unitType: '', cause: null, level: null, fy: scenario.fiscal_year }
  const go = (patch: { segment?: number | null; unit?: number | null }) => {
    const q = new URLSearchParams({ tab: 'overview' })
    const segment = 'segment' in patch ? patch.segment : f.segment
    const unit = 'unit' in patch ? patch.unit : f.unit
    if (segment != null) q.set('segment', String(segment))
    if (unit != null) q.set('unit', String(unit))
    navigate(`/scenarios/${scenario.id}?${q}`)
  }

  const reportQuery = query({ scenario_ids: String(scenario.id) })
  const unitReport = useApi<ComparisonReport>(`/reports/comparison${reportQuery}`)
  const risk = useApi<RiskReport>(`/reports/risk${query({ scenario_id: scenario.id })}`)
  const status = useApi<ActivityProgressReport>(`/scenarios/${scenario.id}/activity-status?scope=all`)
  const tree = useMemo(() => buildTree(segments), [segments])
  const statusById = useMemo(() => new Map((status.data?.items ?? []).map((it) => [it.activity_id, it])), [status.data])
  const dashActivities: DashActivity[] = useMemo(
    () => activities.map((a) => ({ id: a.id, code: a.code, name: a.name, unit_id: a.unit_id, owner_user_id: a.owner_user_id, activity_type: a.activity_type, confidence_level: a.confidence_level, causes: [] })),
    [activities],
  )
  const scope = scopeOf(f, tree, units, dashActivities, me)
  const activityRows = useActivityRows(reportQuery, [...scope.units].sort((a, b) => a - b))

  const err = unitReport.error ?? risk.error ?? status.error ?? activityRows.error
  const key = unitReport.data?.series.find((s) => s.scenario_id === scenario.id)?.key
  const ready = unitReport.data && risk.data && status.data && activityRows.rows && key
  return (
    <AmountUnitProvider unit={amountUnit}>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <Crumbs f={f} tree={tree} units={units} go={go} />
        <AmountUnitSwitch value={amountUnit} onChange={setAmountUnit} />
      </div>
      {err ? (
        <ErrorMessage error={err} />
      ) : !ready ? (
        <Loading />
      ) : (
        <OverviewBody
          scenario={scenario}
          f={f}
          tree={tree}
          units={units}
          subjects={subjects}
          activities={activities}
          dashActivities={dashActivities}
          scope={scope}
          rows={scopedRows(unitReport.data!.rows, activityRows.rows!, scope)}
          restricted={unitReport.data!.restricted_hidden}
          dataKey={key!}
          risk={risk.data!}
          statusById={statusById}
          measure={measure}
          onMeasure={setMeasure}
          go={go}
        />
      )}
    </AmountUnitProvider>
  )
}

function OverviewBody({
  scenario,
  f,
  tree,
  units,
  subjects,
  activities,
  dashActivities,
  scope,
  rows,
  restricted,
  dataKey,
  risk,
  statusById,
  measure,
  onMeasure,
  go,
}: {
  scenario: Scenario
  f: DashFilters
  tree: Tree
  units: Unit[]
  subjects: Subject[]
  activities: Activity[]
  dashActivities: DashActivity[]
  scope: ReturnType<typeof scopeOf>
  rows: ReturnType<typeof scopedRows>
  restricted: boolean
  dataKey: string
  risk: RiskReport
  statusById: Map<number, ActivityProgressReport['items'][number]>
  measure: Measure
  onMeasure: (m: Measure) => void
  go: (patch: { segment?: number | null; unit?: number | null }) => void
}) {
  const { fmt } = useAmountUnit()
  const months = fiscalMonths(scenario.fiscal_year)
  const subjectById = new Map(subjects.map((s) => [s.id, s]))
  const categoryOf = (id: number) => subjectById.get(id)?.category
  const total = totalsBySeries(rows, [dataKey], categoryOf, months).get(dataKey)!
  const unitName = new Map(units.map((u) => [u.id, u.name]))
  const activityById = new Map(activities.map((a) => [a.id, a]))

  if (scope.activities.size === 0 && !scope.withUnallocated) {
    return (
      <div className="space-y-4">
        <ScenarioNarrative scenario={scenario} aside={null} />
        <Card>
          <Empty>この範囲に施策はありません。</Empty>
        </Card>
      </div>
    )
  }

  // ④ 計画の性質・ひとことで
  const revenue = revenueByActivity(rows, dataKey, categoryOf, months)
  const yearRevenue = total.year.revenue
  const byType = revenueByType(revenue, dashActivities)
  const typeParts = typeOrder.map((t) => ({ key: t, label: activityTypeLabels[t], color: typeColor[t], value: byType.get(t) ?? 0n }))
  const biggestType = [...typeParts].sort((a, b) => (a.value === b.value ? 0 : a.value > b.value ? -1 : 1))[0]
  const riskItems = risk.activities.filter((a) => scope.activities.has(a.id))
  const composition = sumComposition(riskItems, compositionKeys(risk.levels))
  const sure = highCertaintyRate(composition, risk.levels)
  const half = halves(total.byMonth, months)
  const nv = newVsContinuing(revenue, (id) => activityById.get(id)?.start_date, `${months[0]}-01`)
  const outlook = { base: 0n, addon: 0n, downside: 0n }
  for (const a of riskItems) {
    for (const l of a.lines) if (categoryOf(l.subject_id) === 'revenue') outlook[l.outlook] += BigInt(l.amount)
  }
  const sentences = summarySentences(
    {
      revenue: yearRevenue,
      profit: profitOf(total.year),
      topType: biggestType && biggestType.value > 0n ? { label: biggestType.label, rate: shareRate(biggestType.value, yearRevenue) ?? 0 } : null,
      top5: topRate(revenue, 5),
      sure,
      secondHalf: shareRate(half.second, half.first + half.second),
      newRate: shareRate(nv.new, nv.new + nv.continuing),
    },
    fmt,
  )

  // ② 何でできているか
  const groups = childGroups(f, tree, units, dashActivities, scope)
  const byGroup = groupTotals(groups, rows, (g) => groupIncludes(g, tree, units), dataKey, categoryOf, months)
  const targetOf = (g: Group): number | (() => void) | null =>
    g.kind === 'activity' ? g.id : g.kind === 'segment' ? () => go({ segment: g.id, unit: null }) : g.kind === 'unit' ? () => go({ unit: g.id }) : null
  const hintOf = (g: Group) => (g.kind === 'activity' ? 'クリックで施策を開く' : g.kind === 'unallocated' ? undefined : 'クリックで掘り下げる')
  const revenueShares = topShares(byGroup.map(({ group, pl }) => ({ key: group.key, label: group.name, value: pl.revenue, group })))
  const profitBars = byGroup
    .filter(({ pl }) => pl.revenue !== 0n || pl.expense !== 0n)
    .map(({ group, pl }) => ({ key: group.key, label: group.name, value: profitOf(pl), target: targetOf(group), hint: hintOf(group) }))
    .sort((a, b) => (a.value === b.value ? 0 : a.value > b.value ? -1 : 1))
  const level = f.unit !== null ? '施策ごと' : groups.some((g) => g.kind === 'segment') ? 'セグメントごと' : 'ユニットごと'

  // ⑤ 前提と山場
  const top10 = [...revenue.entries()]
    .filter(([id, v]) => id !== 0 && v > 0n && activityById.has(id))
    .sort((a, b) => (a[1] === b[1] ? 0 : a[1] > b[1] ? -1 : 1))
    .slice(0, 10)
  const scoped = activities.filter((a) => scope.activities.has(a.id))

  const pl = plStatement(rows, dataKey, subjects, months, scenario.actual_through)

  // 数字から見えること（説明の材料）
  const growth = growthHints(
    byGroup.map(({ group, pl }) => ({ key: group.key, label: group.name, value: pl.revenue })),
    revenue,
    (id) => activityById.get(id)?.is_priority ?? false,
    (id) => activityById.get(id)?.start_date,
    `${months[0]}-01`,
  )
  const numbers = (compact: boolean) => (
    <NumbersSay
      compact={compact}
      title={`数字から見えること（${scenarioLabel(scenario)}・年間）`}
      sentences={sentences}
      range={sumMeasures(riskItems)}
      growth={growth}
      risks={riskHints(riskItems, risk.levels, revenue)}
      nameOf={(id) => activityById.get(id)?.name ?? ''}
      linkOf={(id) => `/activities/${id}?tab=overview&scenario=${scenario.id}`}
    />
  )

  return (
    <div className="space-y-4">
      <RestrictedNote hidden={restricted} />
      <ScenarioNarrative scenario={scenario} aside={numbers(true)} />
      {numbers(false)}

      <Section title="年間の P/L" sub={`${scenario.fiscal_year}年度・実績の月は実績`}>
        <Card>
          <div className="overflow-x-auto">
            <Table>
              <thead>
                <tr>
                  <th className="whitespace-nowrap">科目</th>
                  {pl.quarters.map((q) => (
                    <th key={q.key} className="text-right whitespace-nowrap">
                      {q.label}
                      {q.partlyActual && <span className="ml-1 text-[10px] font-normal text-slate-500">一部実績</span>}
                    </th>
                  ))}
                  <th className="bg-slate-50 text-right">年間</th>
                </tr>
              </thead>
              <tbody>
                {pl.lines.map((l) => (
                  <tr key={l.key} className={cx(l.kind !== 'subject' && 'font-semibold', l.kind === 'profit' && 'bg-slate-50')}>
                    <td className={cx('whitespace-nowrap', l.kind === 'subject' && 'pl-6 text-slate-700')}>{l.label}</td>
                    {[...pl.quarters.map((q) => q.key), 'year'].map((k) => {
                      const v = l.values.get(k)
                      return (
                        <td key={k} className={cx('text-right whitespace-nowrap tabular-nums', k === 'year' && 'bg-slate-50')}>
                          {l.kind === 'margin' ? (v == null ? '—' : formatRate(v as number).replace(/^\+/, '')) : fmt(v as bigint)}
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>
        </Card>
      </Section>

      <div className="grid gap-4 xl:grid-cols-2">
        <Section title="何でできているか（売上）" sub={level}>
          {revenueShares.length === 0 ? (
            <Card>
              <Empty>売上のある区分がありません。</Empty>
            </Card>
          ) : (
            <AmountBars
              title="売上"
              kind="size"
              bars={revenueShares.map((s) => ({
                key: s.key,
                label: s.label,
                value: s.value,
                share: shareRate(s.value, yearRevenue),
                target: s.group ? targetOf(s.group) : null,
                hint: s.group ? hintOf(s.group) : undefined,
              }))}
              footer="年間の売上の大きい順です。区分を押すと、そこに掘り下げます。"
            />
          )}
        </Section>
        <Section title="何でできているか（利益）" sub={level}>
          {profitBars.length === 0 ? (
            <Card>
              <Empty>金額のある区分がありません。</Empty>
            </Card>
          ) : (
            <AmountBars title="利益" kind="signed" bars={profitBars} footer="年間の利益です。共通費・管理部門の区分は赤字（費用）として出ます。" />
          )}
        </Section>
        <Section title="1年の形" sub={`上期 ${shareRate(half.first, half.first + half.second) ?? '—'}%・下期 ${shareRate(half.second, half.first + half.second) ?? '—'}%（売上）`}>
          <TrendChart
            months={months}
            actualThrough={scenario.actual_through}
            series={{ current: total.byMonth, compare: null, previous: null }}
            measure={measure}
            onMeasure={onMeasure}
            labels={{ current: scenario.name, compare: '', previous: '' }}
          />
        </Section>
        <Section title="計画の性質" sub="売上の割合">
          <Card>
            <div className="space-y-4">
              <ShareStack title="施策のタイプ別" parts={typeParts} />
              <div>
                <h4 className="mb-1 text-xs font-semibold text-slate-600">確度別（実績・段階）</h4>
                <CompositionBar composition={composition} levels={risk.levels} />
              </div>
              <div className="grid gap-2 sm:grid-cols-3">
                <Stat label="見通しの種類" value={`ベース ${shareRate(outlook.base, outlook.base + outlook.addon) ?? '—'}%`} hint={`アドオン ${shareRate(outlook.addon, outlook.base + outlook.addon) ?? '—'}%・ダウンサイド ${fmt(outlook.downside)}`} />
                <Stat label="年度の途中で始まる施策" value={`${shareRate(nv.new, nv.new + nv.continuing) ?? '—'}%`} hint={`売上 ${fmt(nv.new)}`} />
                <Stat label="上位への集中" value={`上位5 ${topRate(revenue, 5) ?? '—'}%`} hint={`上位10 ${topRate(revenue, 10) ?? '—'}%`} />
              </div>
            </div>
          </Card>
        </Section>
      </div>

      <Section title="前提と山場" sub="売上の大きい施策の前提条件と説明、期間とマイルストーン">
        <div className="space-y-4">
          <Card>
            {top10.length === 0 ? (
              <Empty>売上のある施策はありません。</Empty>
            ) : (
              <ol className="grid gap-2 lg:grid-cols-2" aria-label="売上の大きい施策">
                {top10.map(([id, v], i) => {
                  const a = activityById.get(id)!
                  const note = statusById.get(id)?.explanation
                  return (
                    <li key={id} className="rounded-md border border-slate-100 px-3 py-2 text-xs">
                      <div className="flex items-baseline justify-between gap-2">
                        <Link to={`/activities/${id}?tab=overview&scenario=${scenario.id}`} className="font-medium text-indigo-700 hover:underline">
                          {i + 1}. {a.name}
                        </Link>
                        <span className="tabular-nums text-slate-700">{fmt(v)}</span>
                      </div>
                      <div className="mt-0.5 text-slate-500">
                        {unitName.get(a.unit_id)}・{activityTypeLabels[a.activity_type]}
                      </div>
                      <p className="mt-1 text-slate-700">
                        <span className="text-slate-500">前提: </span>
                        {a.assumptions || <span className="text-slate-400">未入力</span>}
                      </p>
                      {note && (
                        <p className="mt-0.5 line-clamp-2 text-slate-700">
                          <span className="text-slate-500">説明: </span>
                          {note}
                        </p>
                      )}
                    </li>
                  )
                })}
              </ol>
            )}
          </Card>
          <TimelineChart activities={scoped} scenario={scenario} unitName={(id) => unitName.get(id) ?? ''} />
        </div>
      </Section>
      <p className="text-xs text-slate-500">見通しの種類は、内訳の計画値（{Object.values(outlookLabels).join('・')}）から出しています。差（目標・前回の見込との比較）は、ダッシュボードと予実比較で見られます。</p>
    </div>
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

function Crumbs({ f, tree, units, go }: { f: DashFilters; tree: Tree; units: Unit[]; go: (patch: { segment?: number | null; unit?: number | null }) => void }) {
  const crumbs = breadcrumb(f, tree, units)
  return (
    <nav aria-label="掘り下げの位置" className="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm">
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
              <button type="button" className="text-indigo-700 hover:underline" onClick={() => go({ segment: c.segment, unit: c.unit })}>
                {c.label}
              </button>
            )}
          </span>
        )
      })}
    </nav>
  )
}

/**
 * 数字から見えること（docs/plan.md「2.31」）。要約・業績の幅・伸びの候補・リスクの候補を、既存のデータから並べる。
 * 説明の編集中は、入力欄の横にも同じものを出す（狭い幅では1列に並ぶ）。
 */
function NumbersSay({
  compact,
  title,
  sentences,
  range,
  growth,
  risks,
  nameOf,
  linkOf,
}: {
  compact: boolean
  title: string
  sentences: string[]
  range: Totals
  growth: GrowthHints
  risks: RiskHints
  nameOf: (id: number) => string
  linkOf: (id: number) => string
}) {
  const { fmt, signed } = useAmountUnit()
  const act = (id: number) => (
    <Link to={linkOf(id)} className="text-indigo-700 hover:underline">
      {nameOf(id)}
    </Link>
  )
  const growthItems: ReactNode[] = []
  if (growth.groups.length > 0)
    growthItems.push(
      <>
        売上の大きい区分: {growth.groups.map((g) => `${g.label} ${fmt(g.value)}${g.share == null ? '' : `（${g.share}%）`}`).join('、')}
      </>,
    )
  if (growth.priority)
    growthItems.push(
      <>
        重点施策で {fmt(growth.priority.value)}
        {growth.priority.share == null ? '' : `（${growth.priority.share}%）`}:{' '}
        {growth.priority.top.map((r, i) => (
          <span key={r.id}>
            {i > 0 && '、'}
            {act(r.id)}
          </span>
        ))}
      </>,
    )
  if (growth.newActivities.length > 0)
    growthItems.push(
      <>
        年度の途中で始まる施策:{' '}
        {growth.newActivities.map((r, i) => (
          <span key={r.id}>
            {i > 0 && '、'}
            {act(r.id)} {fmt(r.value)}
          </span>
        ))}
      </>,
    )
  const riskItems: ReactNode[] = []
  if (risks.downside !== 0n) riskItems.push(<>ダウンサイド（見通しの種類）の売上 {signed(risks.downside)}</>)
  if (risks.lowConfidence.length > 0)
    riskItems.push(
      <>
        確度の低い段階の売上が大きい施策:{' '}
        {risks.lowConfidence.map((r, i) => (
          <span key={r.id}>
            {i > 0 && '、'}
            {act(r.id)} {fmt(r.value)}
          </span>
        ))}
      </>,
    )
  for (const [label, list] of [
    ['確度の高い段階なのに警告がある施策', risks.warned.filter((w) => w.badForLevel)],
    ['警告のある施策', risks.warned.filter((w) => !w.badForLevel)],
  ] as const) {
    if (list.length === 0) continue
    riskItems.push(
      <>
        {label}:{' '}
        {list.map((w, i) => (
          <span key={w.id}>
            {i > 0 && '、'}
            {act(w.id)}
            {w.count > 1 && `（警告 ${w.count}）`}
          </span>
        ))}
      </>,
    )
  }
  const rangeRows = [
    { key: 'optimistic', label: '楽観' },
    { key: 'weighted', label: '加重見込' },
    { key: 'pessimistic', label: '悲観' },
  ] as const
  return (
    <section aria-label="数字から見えること" className="rounded-lg border border-indigo-200 bg-indigo-50/60 px-4 py-3">
      <h2 className="mb-1 text-xs font-semibold text-indigo-800">{title}</h2>
      <p className="text-sm leading-relaxed text-slate-800">{sentences.join('')}</p>
      <div className={cx('mt-3 grid gap-4 text-xs', !compact && 'md:grid-cols-3')}>
        <div aria-label="業績の幅" role="group">
          <h3 className="mb-1 font-semibold text-slate-700">業績の幅</h3>
          <table className="w-full tabular-nums">
            <thead>
              <tr className="text-slate-500">
                <th className="text-left font-normal" />
                <th className="text-right font-normal">売上</th>
                <th className="text-right font-normal">利益</th>
              </tr>
            </thead>
            <tbody>
              {rangeRows.map((r) => (
                <tr key={r.key}>
                  <td className="text-slate-600">{r.label}</td>
                  <td className="text-right text-slate-800">{fmt(range[r.key].revenue)}</td>
                  <td className="text-right text-slate-800">{fmt(range[r.key].revenue - range[r.key].expense)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <HintList title="伸びの候補" items={growthItems} />
        <HintList title="リスクの候補" items={riskItems} />
      </div>
    </section>
  )
}

function HintList({ title, items }: { title: string; items: ReactNode[] }) {
  return (
    <div aria-label={title} role="group" className="min-w-0">
      <h3 className="mb-1 font-semibold text-slate-700">{title}</h3>
      {items.length === 0 ? (
        <p className="text-slate-500">目立つものはありません。</p>
      ) : (
        <ul className="list-disc space-y-1 pl-4 text-slate-800">
          {items.map((it, i) => (
            <li key={i}>{it}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
