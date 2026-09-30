import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { api, query } from '../../api/client'
import {
  categoryLabels,
  scenarioKindLabels,
  type Activity,
  type ComparisonReport,
  type FunctionItem,
  type List,
  type ReportRow,
  type ReportSeries,
  type Scenario,
  type Subject,
  type TreeNode,
} from '../../api/types'
import { Button, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../../components/ui'
import { aggregate, isFavorable, measureLabels, measureOf, varianceRate, type Measure, type Totals } from '../../lib/aggregate'
import { formatYen, monthLabel } from '../../lib/format'
import { Link, navigate, useLocation } from '../../lib/router'
import { buildTree, subtreeIds, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

type Axis = 'segment' | 'organization'

/** 表の1行（階層ノード・機能・施策） */
type Node = {
  key: string
  type: 'tree' | 'function' | 'activity'
  id: number
  name: string
  depth: number
  children: Node[]
  include: (row: ReportRow) => boolean
  /** 施策の行は、機能ごとに取得した施策別の行から集計する */
  source: 'function' | 'activity'
}

type Settings = {
  fy: number
  base: number | null
  compare: number[]
  landing: { actual: number; forecast: number; through: string } | null
  axis: Axis
  measure: Measure
  period: string
}

export function ReportPage() {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const segments = useApi<List<TreeNode>>('/segments')
  const organizations = useApi<List<TreeNode>>('/organizations')
  const functions = useApi<List<FunctionItem>>('/functions')
  const subjects = useApi<List<Subject>>('/subjects')
  const activities = useApi<List<Activity>>('/activities')

  const error = scenarios.error ?? segments.error ?? organizations.error ?? functions.error ?? subjects.error ?? activities.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !segments.data || !organizations.data || !functions.data || !subjects.data || !activities.data) return <Loading />

  return (
    <ReportView
      scenarios={scenarios.data.items}
      segments={segments.data.items}
      organizations={organizations.data.items}
      functions={functions.data.items}
      subjects={subjects.data.items}
      activities={activities.data.items}
    />
  )
}

/** URL のクエリから設定を読む。未指定の項目は既定値にする */
function useSettings(scenarios: Scenario[]): [Settings, (patch: Partial<Settings>) => void] {
  const { search } = useLocation()
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const fy = Number(search.get('fy')) || years[0] || 0
  const inYear = scenarios.filter((s) => s.fiscal_year === fy)
  const defaultBase = inYear.find((s) => s.scenario_kind === 'budget') ?? inYear[0]
  const base = search.has('base') ? Number(search.get('base')) || null : defaultBase?.id ?? null
  const compare = (search.get('cmp') ?? '').split(',').filter(Boolean).map(Number)
  const [la, lf, lt] = [search.get('la'), search.get('lf'), search.get('lt')]
  const settings: Settings = {
    fy,
    base,
    compare,
    landing: la && lf && lt ? { actual: Number(la), forecast: Number(lf), through: lt } : null,
    axis: search.get('axis') === 'organization' ? 'organization' : 'segment',
    measure: (['profit', 'revenue', 'expense'] as const).find((m) => m === search.get('measure')) ?? 'profit',
    period: search.get('period') ?? 'year',
  }
  const update = (patch: Partial<Settings>) => {
    const s = { ...settings, ...patch }
    navigate(
      `/reports${query({
        fy: s.fy,
        base: s.base ?? '',
        cmp: s.compare.join(','),
        la: s.landing?.actual,
        lf: s.landing?.forecast,
        lt: s.landing?.through,
        axis: s.axis,
        measure: s.measure,
        period: s.period,
      })}`,
      { replace: true },
    )
  }
  return [settings, update]
}

function ReportView({
  scenarios,
  segments,
  organizations,
  functions,
  subjects,
  activities,
}: {
  scenarios: Scenario[]
  segments: TreeNode[]
  organizations: TreeNode[]
  functions: FunctionItem[]
  subjects: Subject[]
  activities: Activity[]
}) {
  const [settings, update] = useSettings(scenarios)
  const inYear = scenarios.filter((s) => s.fiscal_year === settings.fy)
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)

  const scenarioIds = [settings.base, ...settings.compare].filter((id): id is number => id !== null && inYear.some((s) => s.id === id))
  const landingValid = settings.landing && inYear.some((s) => s.id === settings.landing!.actual) && inYear.some((s) => s.id === settings.landing!.forecast)
  const reportQuery =
    scenarioIds.length > 0 || landingValid
      ? query({
          scenario_ids: scenarioIds.join(','),
          landing_actual_id: landingValid ? settings.landing!.actual : undefined,
          landing_forecast_id: landingValid ? settings.landing!.forecast : undefined,
          landing_through: landingValid ? settings.landing!.through : undefined,
        })
      : null
  const report = useApi<ComparisonReport>(reportQuery ? `/reports/comparison${reportQuery}` : null)

  // 展開した機能の施策別データ（機能 ID → 行）
  const [activityRows, setActivityRows] = useState<Map<number, ReportRow[]>>(new Map())
  const [loadedFor, setLoadedFor] = useState(reportQuery)
  if (loadedFor !== reportQuery) {
    // 条件が変わったら施策別データは取り直す
    setLoadedFor(reportQuery)
    setActivityRows(new Map())
  }
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<Node | null>(null)
  const [hideEmpty, setHideEmpty] = useState(true)
  const [drillError, setDrillError] = useState<unknown>(null)

  const subjectById = useMemo(() => new Map(subjects.map((s) => [s.id, s])), [subjects])
  const categoryOf = (id: number) => subjectById.get(id)?.category
  const tree = useMemo(() => buildTree(settings.axis === 'segment' ? segments : organizations), [settings.axis, segments, organizations])

  const data = report.data
  const seriesKeys = data?.series.map((s) => s.key) ?? []
  const months = useMemo(() => data?.months ?? [], [data])
  const periodMonths = useMemo(() => new Set(periodToMonths(settings.period, months)), [settings.period, months])

  const roots = useMemo(() => buildNodes(tree, settings.axis, functions, activities, activityRows), [tree, settings.axis, functions, activities, activityRows])

  const totalsOf = (n: Node): Map<string, Totals> => {
    const rows = n.source === 'activity' ? activityRows.get(functionOfActivity(n, activities)) ?? [] : data?.rows ?? []
    return aggregate(rows, seriesKeys, categoryOf, n.include, periodMonths)
  }

  const toggle = async (n: Node) => {
    const next = new Set(expanded)
    if (next.has(n.key)) {
      next.delete(n.key)
    } else {
      next.add(n.key)
      if (n.type === 'function' && !activityRows.has(n.id) && reportQuery) {
        try {
          const res = await api.get<ComparisonReport>(`/reports/comparison${reportQuery}&function_id=${n.id}`)
          setActivityRows((prev) => new Map(prev).set(n.id, res.rows))
        } catch (err) {
          setDrillError(err)
          return
        }
      }
    }
    setExpanded(next)
  }

  const scenarioLabel = (s: Scenario) => `${s.name}（${scenarioKindLabels[s.scenario_kind]}${s.is_locked ? '・ロック' : ''}）`
  const actuals = inYear.filter((s) => s.scenario_kind === 'actual')
  const forecasts = inYear.filter((s) => s.scenario_kind !== 'actual')

  // 全体（ルートの合計）
  const allTotals = data ? aggregate(data.rows, seriesKeys, categoryOf, () => true, periodMonths) : null

  return (
    <>
      <PageHeader title="予実比較" description="シナリオ（予算・見込・実績）と着地見込を並べ、セグメント・組織の階層で比較します。1つ目の系列が差異の基準です。" />

      <Card className="mb-4">
        <div className="grid gap-3 md:grid-cols-4">
          <Control label="年度">
            <Select value={settings.fy} onChange={(e) => update({ fy: Number(e.target.value), base: null, compare: [], landing: null })}>
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          </Control>
          <Control label="比較の基準">
            <Select value={settings.base ?? ''} onChange={(e) => update({ base: e.target.value ? Number(e.target.value) : null })}>
              <option value="">（なし）</option>
              {inYear.map((s) => (
                <option key={s.id} value={s.id}>
                  {scenarioLabel(s)}
                </option>
              ))}
            </Select>
          </Control>
          {[0, 1].map((i) => (
            <Control key={i} label={`比較対象 ${i + 1}`}>
              <Select
                value={settings.compare[i] ?? ''}
                onChange={(e) => {
                  const next = [...settings.compare]
                  if (e.target.value) next[i] = Number(e.target.value)
                  else next.splice(i, 1)
                  update({ compare: next.filter(Boolean) })
                }}
              >
                <option value="">（なし）</option>
                {inYear.map((s) => (
                  <option key={s.id} value={s.id}>
                    {scenarioLabel(s)}
                  </option>
                ))}
              </Select>
            </Control>
          ))}
        </div>

        <div className="mt-3 grid gap-3 border-t border-slate-100 pt-3 md:grid-cols-4">
          <label className="flex items-center gap-2 text-sm font-medium text-slate-700 md:col-span-4">
            <input
              type="checkbox"
              className="size-4 rounded border-slate-300"
              checked={settings.landing !== null}
              disabled={actuals.length === 0 || forecasts.length === 0}
              onChange={(e) =>
                update({
                  // 見込は、種別が「見込」の最新のシナリオを初期値にする（一覧は新しい順）
                  landing: e.target.checked
                    ? { actual: actuals[0].id, forecast: (forecasts.find((s) => s.scenario_kind === 'forecast') ?? forecasts[0]).id, through: lastClosedMonth(settings.fy) }
                    : null,
                })
              }
            />
            着地見込を加える（実績＋見込）
            {(actuals.length === 0 || forecasts.length === 0) && <span className="text-xs font-normal text-slate-400">この年度に実績シナリオと見込シナリオが必要です</span>}
          </label>
          {settings.landing && (
            <>
              <Control label="実績">
                <Select value={settings.landing.actual} onChange={(e) => update({ landing: { ...settings.landing!, actual: Number(e.target.value) } })}>
                  {actuals.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              </Control>
              <Control label="見込">
                <Select value={settings.landing.forecast} onChange={(e) => update({ landing: { ...settings.landing!, forecast: Number(e.target.value) } })}>
                  {forecasts.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </Select>
              </Control>
              <Control label="実績を使う最後の月">
                <Select value={settings.landing.through} onChange={(e) => update({ landing: { ...settings.landing!, through: e.target.value } })}>
                  {fiscalMonths(settings.fy).map((m) => (
                    <option key={m} value={m}>
                      {m.replace('-', '年')}月まで
                    </option>
                  ))}
                </Select>
              </Control>
            </>
          )}
        </div>

        <div className="mt-3 flex flex-wrap items-end gap-3 border-t border-slate-100 pt-3">
          <Segmented label="集計軸" value={settings.axis} options={{ segment: 'セグメント', organization: '組織' }} onChange={(v) => update({ axis: v as Axis })} />
          <Segmented label="指標" value={settings.measure} options={measureLabels} onChange={(v) => update({ measure: v as Measure })} />
          <Control label="期間">
            <Select value={settings.period} onChange={(e) => update({ period: e.target.value })} className="w-36">
              <option value="year">年間</option>
              <option value="h1">上期（4〜9月）</option>
              <option value="h2">下期（10〜3月）</option>
              {fiscalMonths(settings.fy).map((m) => (
                <option key={m} value={m}>
                  {monthLabel(m)}
                </option>
              ))}
            </Select>
          </Control>
          <label className="mb-2 flex items-center gap-2 text-sm text-slate-700">
            <input type="checkbox" className="size-4 rounded border-slate-300" checked={hideEmpty} onChange={(e) => setHideEmpty(e.target.checked)} />
            金額のない行を隠す
          </label>
        </div>
      </Card>

      {!reportQuery ? (
        <Card>
          <Empty>比較するシナリオを選んでください</Empty>
        </Card>
      ) : report.error ? (
        <ErrorMessage error={report.error} />
      ) : !data ? (
        <Loading />
      ) : (
        <>
          {drillError ? (
            <div className="mb-4">
              <ErrorMessage error={drillError} />
            </div>
          ) : null}
          <Card className="mb-4" title={`${measureLabels[settings.measure]}（円）・${periodLabel(settings.period)}`}>
            <div className="-mx-4 overflow-x-auto">
              <table className="min-w-full border-separate border-spacing-0 text-sm">
                <thead>
                  <tr className="text-xs text-slate-500">
                    <th className="sticky left-0 z-10 min-w-64 border-b border-slate-200 bg-white px-3 py-2 text-left font-semibold">
                      {settings.axis === 'segment' ? 'セグメント' : '組織'} / 機能 / 施策
                    </th>
                    {data.series.map((s, i) => (
                      <SeriesHeaders key={s.key} series={s} isBase={i === 0} />
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {allTotals && (
                    <ValueRow
                      label={<span className="font-semibold">全体</span>}
                      totals={allTotals}
                      series={data.series}
                      measure={settings.measure}
                      depth={0}
                      selected={selected === null}
                      onSelect={() => setSelected(null)}
                      className="bg-slate-50"
                    />
                  )}
                  <NodeRows
                    nodes={roots}
                    expanded={expanded}
                    totalsOf={totalsOf}
                    series={data.series}
                    measure={settings.measure}
                    hideEmpty={hideEmpty}
                    selected={selected}
                    onSelect={setSelected}
                    onToggle={toggle}
                  />
                </tbody>
              </table>
            </div>
          </Card>
          <DetailPanel
            node={selected}
            totals={selected ? totalsOf(selected) : allTotals!}
            series={data.series}
            subjects={subjects}
            months={months}
            measure={settings.measure}
            periodLabel={periodLabel(settings.period)}
          />
        </>
      )}
    </>
  )
}

// --- 表の行 ---

function SeriesHeaders({ series, isBase }: { series: ReportSeries; isBase: boolean }) {
  return (
    <>
      <th className="border-b border-slate-200 px-3 py-2 text-right font-semibold whitespace-nowrap">
        {series.label}
        {isBase && <span className="ml-1 rounded bg-slate-100 px-1 text-[10px] text-slate-500">基準</span>}
      </th>
      {!isBase && <th className="border-b border-slate-200 px-3 py-2 text-right font-semibold whitespace-nowrap">差異</th>}
    </>
  )
}

function NodeRows({
  nodes,
  expanded,
  totalsOf,
  series,
  measure,
  hideEmpty,
  selected,
  onSelect,
  onToggle,
}: {
  nodes: Node[]
  expanded: Set<string>
  totalsOf: (n: Node) => Map<string, Totals>
  series: ReportSeries[]
  measure: Measure
  hideEmpty: boolean
  selected: Node | null
  onSelect: (n: Node) => void
  onToggle: (n: Node) => void
}) {
  return (
    <>
      {nodes.map((n) => {
        const totals = totalsOf(n)
        const empty = [...totals.values()].every((t) => t.revenue === 0n && t.expense === 0n)
        if (hideEmpty && empty && n.type !== 'tree') return null
        if (hideEmpty && empty && n.type === 'tree' && !hasFunctions(n)) return null
        const expandable = n.type !== 'activity' && (n.children.length > 0 || n.type === 'function')
        const isOpen = expanded.has(n.key)
        return (
          <Fragment key={n.key}>
            <ValueRow
              label={
                <span className="flex items-center gap-1">
                  {expandable ? (
                    <button
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation()
                        onToggle(n)
                      }}
                      aria-expanded={isOpen}
                      aria-label={isOpen ? `${n.name}を閉じる` : `${n.name}を開く`}
                      className="w-5 rounded text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                      {isOpen ? '▾' : '▸'}
                    </button>
                  ) : (
                    <span className="w-5" />
                  )}
                  {n.type === 'function' && <span className="shrink-0 rounded bg-indigo-50 px-1 text-[10px] whitespace-nowrap text-indigo-700">機能</span>}
                  {n.type === 'activity' && <span className="shrink-0 rounded bg-slate-100 px-1 text-[10px] whitespace-nowrap text-slate-600">施策</span>}
                  <span className={cx(n.type === 'tree' && 'font-medium', empty && 'text-slate-400')}>{n.name}</span>
                </span>
              }
              totals={totals}
              series={series}
              measure={measure}
              depth={n.depth}
              selected={selected?.key === n.key}
              onSelect={() => onSelect(n)}
            />
            {isOpen && (
              <NodeRows
                nodes={n.children}
                expanded={expanded}
                totalsOf={totalsOf}
                series={series}
                measure={measure}
                hideEmpty={hideEmpty}
                selected={selected}
                onSelect={onSelect}
                onToggle={onToggle}
              />
            )}
          </Fragment>
        )
      })}
    </>
  )
}

function hasFunctions(n: Node): boolean {
  return n.children.some((c) => c.type === 'function' || hasFunctions(c))
}

function ValueRow({
  label,
  totals,
  series,
  measure,
  depth,
  selected,
  onSelect,
  className,
}: {
  label: ReactNode
  totals: Map<string, Totals>
  series: ReportSeries[]
  measure: Measure
  depth: number
  selected: boolean
  onSelect: () => void
  className?: string
}) {
  const base = measureOf(totals.get(series[0].key), measure)
  return (
    <tr onClick={onSelect} className={cx('cursor-pointer hover:bg-indigo-50/40', selected && 'bg-indigo-50', className)}>
      <th scope="row" className={cx('sticky left-0 z-10 border-b border-slate-100 py-1.5 pr-3 text-left font-normal', selected ? 'bg-indigo-50' : 'bg-white', className)} style={{ paddingLeft: 12 + depth * 18 }}>
        {label}
      </th>
      {series.map((s, i) => {
        const v = measureOf(totals.get(s.key), measure)
        return (
          <Fragment key={s.key}>
            <td className="border-b border-slate-100 px-3 py-1.5 text-right tabular-nums">{formatYen(String(v))}</td>
            {i > 0 && <VarianceCell base={base} value={v} measure={measure} />}
          </Fragment>
        )
      })}
    </tr>
  )
}

function VarianceCell({ base, value, measure }: { base: bigint; value: bigint; measure: Measure | 'revenue' | 'expense' }) {
  const diff = value - base
  const fav = isFavorable(diff, measure)
  const rate = varianceRate(base, value)
  return (
    <td className={cx('border-b border-slate-100 px-3 py-1.5 text-right tabular-nums whitespace-nowrap', fav === true && 'text-emerald-700', fav === false && 'text-red-600')}>
      {diff > 0n ? '+' : ''}
      {formatYen(String(diff))}
      {rate !== null && diff !== 0n && <span className="ml-1 text-xs opacity-70">({rate > 0 ? '+' : ''}{rate}%)</span>}
    </td>
  )
}

// --- 詳細 ---

function DetailPanel({
  node,
  totals,
  series,
  subjects,
  months,
  measure,
  periodLabel,
}: {
  node: Node | null
  totals: Map<string, Totals>
  series: ReportSeries[]
  subjects: Subject[]
  months: string[]
  measure: Measure
  periodLabel: string
}) {
  const [view, setView] = useState<'subjects' | 'months'>('subjects')
  const title = node ? node.name : '全体'
  const used = subjects.filter((s) => series.some((x) => totals.get(x.key)?.bySubject.has(s.id)))

  return (
    <Card
      title={`${title} の内訳`}
      actions={
        <div className="flex gap-1">
          <Button size="sm" variant={view === 'subjects' ? 'primary' : 'secondary'} onClick={() => setView('subjects')}>
            科目別（{periodLabel}）
          </Button>
          <Button size="sm" variant={view === 'months' ? 'primary' : 'secondary'} onClick={() => setView('months')}>
            月次推移（{measureLabels[measure]}）
          </Button>
        </div>
      }
    >
      {node?.type === 'activity' && (
        <p className="mb-3 text-sm">
          数値の入力・根拠の確認:{' '}
          {series
            .filter((s) => s.scenario_id)
            .map((s) => (
              <Link key={s.key} to={`/scenarios/${s.scenario_id}/activities/${node.id}`} className="mr-3 text-indigo-700 hover:underline">
                {s.label} →
              </Link>
            ))}
        </p>
      )}
      {view === 'subjects' ? (
        used.length === 0 ? (
          <Empty>金額がありません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>科目</th>
                {series.map((s, i) => (
                  <SeriesHeaders key={s.key} series={s} isBase={i === 0} />
                ))}
              </tr>
            </thead>
            <tbody>
              {(['revenue', 'expense'] as const).map((cat) => (
                <Fragment key={cat}>
                  {used
                    .filter((s) => s.category === cat)
                    .map((s) => {
                      const base = totals.get(series[0].key)?.bySubject.get(s.id) ?? 0n
                      return (
                        <tr key={s.id}>
                          <td>
                            {s.name}
                            <span className="ml-1 text-xs text-slate-400">{categoryLabels[s.category]}</span>
                          </td>
                          {series.map((x, i) => {
                            const v = totals.get(x.key)?.bySubject.get(s.id) ?? 0n
                            return (
                              <Fragment key={x.key}>
                                <td className="text-right tabular-nums">{formatYen(String(v))}</td>
                                {i > 0 && <VarianceCell base={base} value={v} measure={cat} />}
                              </Fragment>
                            )
                          })}
                        </tr>
                      )
                    })}
                </Fragment>
              ))}
              {(['revenue', 'expense', 'profit'] as const).map((m) => {
                const base = measureOf(totals.get(series[0].key), m)
                return (
                  <tr key={m} className="bg-slate-50 font-semibold">
                    <td>{measureLabels[m]}計</td>
                    {series.map((x, i) => {
                      const v = measureOf(totals.get(x.key), m)
                      return (
                        <Fragment key={x.key}>
                          <td className="text-right tabular-nums">{formatYen(String(v))}</td>
                          {i > 0 && <VarianceCell base={base} value={v} measure={m} />}
                        </Fragment>
                      )
                    })}
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )
      ) : (
        <Table>
          <thead>
            <tr>
              <th>系列</th>
              {months.map((m) => (
                <th key={m} className="text-right">
                  {monthLabel(m)}
                </th>
              ))}
              <th className="text-right">年計</th>
            </tr>
          </thead>
          <tbody>
            {series.map((s) => {
              const t = totals.get(s.key)
              const values = months.map((m) => measureOf(t?.byMonth.get(m), measure))
              return (
                <tr key={s.key}>
                  <td className="whitespace-nowrap">{s.label}</td>
                  {values.map((v, i) => (
                    <td key={months[i]} className={cx('text-right tabular-nums', s.actual_through && months[i] <= s.actual_through && 'bg-slate-50')}>
                      {formatYen(String(v))}
                    </td>
                  ))}
                  <td className="text-right font-semibold tabular-nums">{formatYen(String(values.reduce((a, b) => a + b, 0n)))}</td>
                </tr>
              )
            })}
          </tbody>
        </Table>
      )}
      {view === 'months' && series.some((s) => s.kind === 'landing') && <p className="mt-2 text-xs text-slate-500">着地見込の灰色の月は実績です。</p>}
    </Card>
  )
}

// --- 補助 ---

function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-xs font-medium text-slate-500">
      {label}
      <div className="mt-1">{children}</div>
    </label>
  )
}

function Segmented({ label, value, options, onChange }: { label: string; value: string; options: Record<string, string>; onChange: (v: string) => void }) {
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-slate-500">{label}</div>
      <div className="inline-flex rounded-md border border-slate-300 bg-white p-0.5" role="radiogroup" aria-label={label}>
        {Object.entries(options).map(([v, l]) => (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={value === v}
            onClick={() => onChange(v)}
            className={cx('rounded px-3 py-1.5 text-sm', value === v ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
          >
            {l}
          </button>
        ))}
      </div>
    </div>
  )
}

function fiscalMonths(fy: number): string[] {
  return Array.from({ length: 12 }, (_, i) => {
    const m = 4 + i
    const y = m > 12 ? fy + 1 : fy
    return `${y}-${String(m > 12 ? m - 12 : m).padStart(2, '0')}`
  })
}

/** 着地見込の「実績を使う最後の月」の初期値: 前月（年度外なら年度の端） */
function lastClosedMonth(fy: number): string {
  const months = fiscalMonths(fy)
  const d = new Date()
  d.setMonth(d.getMonth() - 1)
  const prev = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  if (prev < months[0]) return months[0]
  if (prev > months[11]) return months[11]
  return prev
}

function periodToMonths(period: string, months: string[]): string[] {
  if (period === 'h1') return months.slice(0, 6)
  if (period === 'h2') return months.slice(6)
  if (period === 'year') return months
  return months.filter((m) => m === period)
}

function periodLabel(period: string): string {
  if (period === 'year') return '年間'
  if (period === 'h1') return '上期'
  if (period === 'h2') return '下期'
  return `${Number(period.slice(5))}月`
}

function functionOfActivity(n: Node, activities: Activity[]): number {
  return activities.find((a) => a.id === n.id)?.function_id ?? 0
}

/** 階層ノード → 機能 → 施策 の木を作る */
function buildNodes(tree: Tree, axis: Axis, functions: FunctionItem[], activities: Activity[], activityRows: Map<number, ReportRow[]>): Node[] {
  const column = axis === 'segment' ? 'segment_id' : 'organization_id'
  const fnByNode = new Map<number, FunctionItem[]>()
  for (const f of functions) {
    const list = fnByNode.get(f[column]) ?? []
    list.push(f)
    fnByNode.set(f[column], list)
  }

  const functionNode = (f: FunctionItem, depth: number): Node => {
    const acts = activityRows.has(f.id) ? activities.filter((a) => a.function_id === f.id) : []
    return {
      key: `f:${f.id}`,
      type: 'function',
      id: f.id,
      name: f.name,
      depth,
      source: 'function',
      include: (row) => row.function_id === f.id,
      children: acts.map((a) => ({
        key: `a:${a.id}`,
        type: 'activity',
        id: a.id,
        name: `${a.name}（${a.code}）`,
        depth: depth + 1,
        source: 'activity',
        include: (row) => row.activity_id === a.id,
        children: [],
      })),
    }
  }

  const treeNode = (t: TreeNode, depth: number): Node => {
    const ids = subtreeIds(tree, t.id)
    const fnIds = new Set(functions.filter((f) => ids.has(f[column])).map((f) => f.id))
    return {
      key: `t:${t.id}`,
      type: 'tree',
      id: t.id,
      name: t.name,
      depth,
      source: 'function',
      include: (row) => fnIds.has(row.function_id),
      children: [...(tree.children.get(t.id) ?? []).map((c) => treeNode(c, depth + 1)), ...(fnByNode.get(t.id) ?? []).map((f) => functionNode(f, depth + 1))],
    }
  }

  return (tree.children.get(null) ?? []).map((t) => treeNode(t, 0))
}
