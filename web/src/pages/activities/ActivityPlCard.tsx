import { useMemo, useState } from 'react'
import type { List, Scenario, ValuesView } from '../../api/types'
import { Card, Empty, ErrorMessage, Loading, Select, cx } from '../../components/ui'
import { isFavorable, varianceRate } from '../../lib/aggregate'
import { formatRate, type AmountUnit } from '../../lib/format'
import { AmountUnitProvider, useAmountUnit } from '../../lib/amountUnit'
import { AmountUnitSwitch } from '../../components/AmountUnitSwitch'
import { buildPl, defaultFiscalYear, grainLabels, periodsOf, sumOver, type Grain, type PlNode, totalsOver } from '../../lib/pl'
import { Link } from '../../lib/router'
import { actualThroughLabel, defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { RestrictedNote } from '../../components/RestrictedNote'
import { buildWip, monthLabel } from '../../lib/wip'

const grains: Grain[] = ['month', 'quarter', 'half', 'year']

/**
 * 施策の P/L。基準シナリオと最新シナリオの金額・差異を、月次・四半期・半期・通期で表示する。
 * 収益・費用は科目 → 内訳へドリルダウンできる。
 */
export function ActivityPlCard({ activityId }: { activityId: number }) {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  if (scenarios.error) return <ErrorMessage error={scenarios.error} />
  if (!scenarios.data) return <Loading />
  return <PlView activityId={activityId} scenarios={scenarios.data.items} />
}

function PlView({ activityId, scenarios }: { activityId: number; scenarios: Scenario[] }) {
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const [fy, setFy] = useState(() => defaultFiscalYear(years))
  const inYear = scenarios.filter((s) => s.fiscal_year === fy)
  const defaults = defaultScenarios(inYear)
  const [picked, setPicked] = useState<{ base?: number; latest?: number }>({})
  const baseId = picked.base ?? defaults.base?.id
  const latestId = picked.latest ?? defaults.latest?.id
  const [grain, setGrain] = useState<Grain>('quarter')
  const [amountUnit, setAmountUnit] = useState<AmountUnit>('million')
  const [open, setOpen] = useState<Set<string>>(new Set())

  const base = useApi<ValuesView>(baseId ? `/scenarios/${baseId}/activities/${activityId}` : null)
  const latest = useApi<ValuesView>(latestId ? `/scenarios/${latestId}/activities/${activityId}` : null)
  // シナリオを切り替えた直後は、前のシナリオのデータを使わない
  const baseData = base.data?.scenario.id === baseId ? base.data : undefined
  const latestData = latest.data?.scenario.id === latestId ? latest.data : undefined

  const nodes = useMemo(() => (baseData && latestData ? buildPl(baseData.amounts, latestData.amounts) : []), [baseData, latestData])
  const months = baseData?.months ?? latestData?.months ?? []
  const periods = periodsOf(months, grain)

  const scenarioOption = (s: Scenario) => (
    <option key={s.id} value={s.id}>
      {scenarioLabel(s)}
    </option>
  )

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      <div role="group" aria-label="表示の単位" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
        {grains.map((g) => (
          <button
            key={g}
            type="button"
            aria-pressed={grain === g}
            onClick={() => setGrain(g)}
            className={cx('rounded px-2.5 py-1 text-xs font-medium', grain === g ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
          >
            {grainLabels[g]}
          </button>
        ))}
      </div>
    </div>
  )

  const error = base.error ?? latest.error
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const expandable = nodes.flatMap(function walk(n: PlNode): string[] {
    return n.children.length > 0 ? [n.id, ...n.children.flatMap(walk)] : []
  })
  const allOpen = expandable.length > 0 && expandable.every((id) => open.has(id))

  return (
    <AmountUnitProvider unit={amountUnit}>
    <Card title="P/L" actions={controls}>
      {years.length === 0 ? (
        <Empty>シナリオがまだありません。</Empty>
      ) : (
        <div className="space-y-3">
          <RestrictedNote hidden={base.data?.restricted_hidden || latest.data?.restricted_hidden} />
          <AmountUnitSwitch value={amountUnit} onChange={setAmountUnit} />
          <div className="flex flex-wrap items-end gap-3 text-xs">
            <label className="space-y-1">
              <span className="block font-medium text-slate-500">年度</span>
              <Select
                aria-label="年度"
                value={fy}
                onChange={(e) => {
                  setFy(Number(e.target.value))
                  setPicked({})
                }}
                className="w-28 py-1 text-xs"
              >
                {years.map((y) => (
                  <option key={y} value={y}>
                    {y}年度
                  </option>
                ))}
              </Select>
            </label>
            <label className="space-y-1">
              <span className="block font-medium text-slate-500">目標</span>
              <Select aria-label="目標のシナリオ" value={baseId ?? ''} onChange={(e) => setPicked((p) => ({ ...p, base: Number(e.target.value) }))} className="w-56 py-1 text-xs">
                {inYear.map(scenarioOption)}
              </Select>
            </label>
            <label className="space-y-1">
              <span className="block font-medium text-slate-500">最新</span>
              <Select aria-label="最新のシナリオ" value={latestId ?? ''} onChange={(e) => setPicked((p) => ({ ...p, latest: Number(e.target.value) }))} className="w-56 py-1 text-xs">
                {inYear.map(scenarioOption)}
              </Select>
            </label>
            <p className="pb-1.5 text-slate-500">
              目標: {actualThroughLabel(inYear.find((s) => s.id === baseId)?.actual_through)} / 最新: {actualThroughLabel(inYear.find((s) => s.id === latestId)?.actual_through)}
            </p>
            <div className="ml-auto flex gap-3 pb-1.5">
              {expandable.length > 0 && (
                <button type="button" onClick={() => setOpen(allOpen ? new Set() : new Set(expandable))} className="text-indigo-700 hover:underline">
                  {allOpen ? 'すべて閉じる' : 'すべて開く'}
                </button>
              )}
              {latestId && (
                <Link to={`/activities/${activityId}?tab=update&scenario=${latestId}`} className="text-indigo-700 hover:underline">
                  数値を入力する →
                </Link>
              )}
            </div>
          </div>

          {error ? (
            <ErrorMessage error={error} />
          ) : !baseData || !latestData ? (
            <Loading />
          ) : nodes.every((n) => n.base.size === 0 && n.latest.size === 0) ? (
            <Empty>この年度の金額はまだありません。</Empty>
          ) : (
            <PlTable nodes={nodes} periods={periods} open={open} onToggle={toggle} />
          )}
          {latestData?.wip && <WipSection view={latestData} />}
          {latestData && latestData.beyond_months.length > 0 && <WholeActivity view={latestData} />}
        </div>
      )}
    </Card>
    </AmountUnitProvider>
  )
}

/**
 * 原価と仕掛（検収月に一括の施策。docs/plan.md「2.28」）: 選んだ「最新」のシナリオの、月ごとの発生原価・仕掛品振替・売上原価・月末仕掛残高。
 * 実績の月は会計の実績（仕掛品振替の実績を含む）。
 */
function WipSection({ view }: { view: ValuesView }) {
  const { fmt, label } = useAmountUnit()
  const table = buildWip(view)
  if (!table) return null
  const recognition = view.wip?.recognition_month ?? null
  const actual = new Set(view.actual_months)
  const rows: { key: string; name: string; values: Map<string, bigint>; total: boolean; strong?: boolean; indent?: boolean }[] = [
    ...table.costs.map((c) => ({ key: `c${c.subjectId}`, name: c.name, values: c.values, total: true, indent: true })),
    { key: 'incurred', name: '発生原価', values: table.incurred, total: true, strong: true },
    { key: 'transfer', name: '仕掛品振替', values: table.transfer, total: true },
    { key: 'cogs', name: '売上原価（P/L）', values: table.cogs, total: true, strong: true },
    { key: 'balance', name: '月末仕掛残高', values: table.balance, total: false, strong: true },
  ]
  return (
    <section aria-label="原価と仕掛" className="border-t border-slate-100 pt-3">
      <h3 className="mb-1 text-xs font-semibold text-slate-500">
        原価と仕掛（{scenarioLabel(view.scenario)}・計上月 {recognition ? monthLabel(recognition) : '未設定'}・{label}）
      </h3>
      {view.wip?.transfer_subject_id == null && <p className="mb-2 text-xs text-amber-700">仕掛品振替の科目が指定されていません。FP&A が「勘定科目」で指定すると、振替を計算します。</p>}
      <div className="-mx-4 overflow-x-auto">
        <table aria-label="原価と仕掛" className="min-w-full border-separate border-spacing-0 text-sm">
          <thead>
            <tr>
              <th className="sticky left-0 z-10 min-w-36 border-b border-slate-200 bg-white px-3 py-1.5 text-left text-xs font-semibold text-slate-500">項目</th>
              {table.months.map((m) => (
                <th
                  key={m}
                  className={cx(
                    'min-w-20 border-b border-slate-200 px-3 py-1.5 text-right text-xs font-semibold whitespace-nowrap text-slate-500',
                    m === recognition && 'bg-indigo-50 text-indigo-700',
                  )}
                >
                  {view.beyond_months.includes(m) ? `翌${Number(m.slice(5, 7))}月` : `${Number(m.slice(5, 7))}月`}
                  {actual.has(m) && <span className="ml-0.5 font-normal">（実績）</span>}
                  {m === recognition && <span className="block text-[10px] font-normal">計上月</span>}
                </th>
              ))}
              <th className="min-w-24 border-b border-slate-200 bg-slate-50 px-3 py-1.5 text-right text-xs font-semibold text-slate-500">合計</th>
            </tr>
          </thead>
          <tbody className="tabular-nums">
            {rows.map((r) => (
              <tr key={r.key} className={cx(r.strong && 'font-semibold', r.key === 'balance' && 'bg-slate-50')}>
                <th scope="row" className={cx('sticky left-0 z-10 border-b border-slate-100 bg-white py-1 pr-3 text-left text-xs whitespace-nowrap text-slate-700', r.indent ? 'pl-7 font-normal' : 'pl-3', r.key === 'balance' && 'bg-slate-50')}>
                  {r.name}
                </th>
                {table.months.map((m) => {
                  const v = r.values.get(m) ?? 0n
                  return (
                    <td key={m} className={cx('border-b border-slate-100 px-3 py-1 text-right whitespace-nowrap', v === 0n && 'text-slate-300', m === recognition && 'bg-indigo-50/60')}>
                      {fmt(v)}
                    </td>
                  )
                })}
                <td className="border-b border-slate-100 bg-slate-50 px-3 py-1 text-right whitespace-nowrap">{r.total ? fmt(table.months.reduce((s, m) => s + (r.values.get(m) ?? 0n), 0n)) : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-1 text-xs text-slate-500">売上原価 = 発生原価 + 仕掛品振替。月末仕掛残高は、前の年度からの残高に、振替の累計を反映したものです。</p>
    </section>
  )
}

/** 施策全体（年度をまたいだ合計。docs/plan.md「2.26」）: 選んだ「最新」のシナリオの、当期の月と翌年度の月の合計 */
function WholeActivity({ view }: { view: ValuesView }) {
  const { fmt, label } = useAmountUnit()
  const current = totalsOver(view.amounts, view.months)
  const beyond = totalsOver(view.amounts, view.beyond_months)
  const cols = [
    { key: 'current', title: `当期（${view.scenario.fiscal_year}年度）`, t: current },
    { key: 'beyond', title: `翌年度（${view.scenario.fiscal_year + 1}年度）`, t: beyond },
    { key: 'total', title: '施策全体', t: { revenue: current.revenue + beyond.revenue, expense: current.expense + beyond.expense } },
  ]
  return (
    <section aria-label="施策全体" className="border-t border-slate-100 pt-3">
      <h3 className="mb-1 text-xs font-semibold text-slate-500">
        施策全体（年度をまたいだ合計・{scenarioLabel(view.scenario)}・{label}）
      </h3>
      <table className="text-sm">
        <thead>
          <tr className="text-xs text-slate-500">
            <th className="py-1 pr-6 text-left font-semibold" />
            {cols.map((c) => (
              <th key={c.key} className="px-3 py-1 text-right font-semibold">
                {c.title}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="tabular-nums">
          {(
            [
              ['売上', (t: (typeof cols)[number]['t']) => t.revenue],
              ['費用', (t: (typeof cols)[number]['t']) => t.expense],
              ['利益', (t: (typeof cols)[number]['t']) => t.revenue - t.expense],
            ] as const
          ).map(([name, f]) => (
            <tr key={name} className={name === '利益' ? 'font-semibold' : undefined}>
              <th scope="row" className="py-1 pr-6 text-left text-xs font-medium text-slate-600">
                {name}
              </th>
              {cols.map((c) => (
                <td key={c.key} className={cx('px-3 py-1 text-right', c.key === 'total' && 'bg-slate-50')}>
                  {fmt(f(c.t))}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

function PlTable({ nodes, periods, open, onToggle }: { nodes: PlNode[]; periods: ReturnType<typeof periodsOf>; open: Set<string>; onToggle: (id: string) => void }) {
  const { fmt, signed } = useAmountUnit()
  const visible: { node: PlNode; depth: number }[] = []
  const walk = (n: PlNode, depth: number) => {
    visible.push({ node: n, depth })
    if (open.has(n.id)) n.children.forEach((c) => walk(c, depth + 1))
  }
  nodes.forEach((n) => walk(n, 0))

  return (
    <div className="-mx-4 overflow-x-auto">
      <table aria-label="P/L" className="min-w-full border-separate border-spacing-0 text-sm">
        <thead>
          <tr>
            <th className="sticky left-0 z-10 min-w-40 border-b border-slate-200 bg-white px-3 py-1.5 text-left text-xs font-semibold text-slate-500">項目</th>
            <th className="border-b border-slate-200 px-2 py-1.5" />
            {periods.map((p) => (
              <th
                key={p.key}
                className={cx(
                  'min-w-24 border-b border-slate-200 px-3 py-1.5 text-right text-xs font-semibold whitespace-nowrap text-slate-500',
                  p.key === 'total' && 'bg-slate-50',
                )}
              >
                {p.label}
              </th>
            ))}
          </tr>
        </thead>
        {visible.map(({ node, depth }) => {
          const strong = depth === 0
          const amounts = periods.map((p) => {
            const b = sumOver(node.base, p.months)
            const l = sumOver(node.latest, p.months)
            return { key: p.key, base: b, latest: l, diff: l - b }
          })
          const rowClass = cx(strong && 'font-semibold', node.id === 'profit' && 'bg-slate-50')
          const cellClass = (key: string) => cx('px-3 py-1 text-right tabular-nums whitespace-nowrap', key === 'total' && 'bg-slate-50')
          return (
            <tbody key={node.id} className="border-t">
              <tr className={rowClass}>
                <th scope="rowgroup" rowSpan={3} className={cx('sticky left-0 z-10 border-b border-slate-200 bg-white py-1 pr-3 text-left align-top', node.id === 'profit' && 'bg-slate-50')}>
                  <div className="flex items-start gap-1" style={{ paddingLeft: `${0.75 + depth * 1.25}rem` }}>
                    {node.children.length > 0 ? (
                      <button
                        type="button"
                        onClick={() => onToggle(node.id)}
                        aria-expanded={open.has(node.id)}
                        aria-label={open.has(node.id) ? `${node.label}を閉じる` : `${node.label}を開く`}
                        className="w-5 shrink-0 rounded text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                      >
                        {open.has(node.id) ? '▾' : '▸'}
                      </button>
                    ) : (
                      <span className="w-5 shrink-0" />
                    )}
                    <div>
                      <div className={cx('whitespace-nowrap text-slate-800', strong ? 'font-semibold' : 'font-medium')}>{node.label}</div>
                      {node.sub && <div className="font-mono text-xs font-normal whitespace-nowrap text-slate-400">{node.sub}</div>}
                    </div>
                  </div>
                </th>
                <td className="px-2 py-1 text-xs font-normal whitespace-nowrap text-slate-500">目標</td>
                {amounts.map((a) => (
                  <td key={a.key} className={cx(cellClass(a.key), a.base === 0n ? 'text-slate-300' : 'text-slate-600')}>
                    {fmt(a.base)}
                  </td>
                ))}
              </tr>
              <tr className={rowClass}>
                <td className="px-2 py-1 text-xs font-normal whitespace-nowrap text-slate-500">最新</td>
                {amounts.map((a) => (
                  <td key={a.key} className={cx(cellClass(a.key), a.latest === 0n ? 'text-slate-300' : 'text-slate-900')}>
                    {fmt(a.latest)}
                  </td>
                ))}
              </tr>
              <tr className={rowClass}>
                <td className="border-b border-slate-200 px-2 py-1 text-xs font-normal whitespace-nowrap text-slate-500">差異</td>
                {amounts.map((a) => {
                  const fav = isFavorable(a.diff, node.measure === 'profit' ? 'profit' : node.measure)
                  const rate = varianceRate(a.base, a.latest)
                  return (
                    <td
                      key={a.key}
                      title={rate === null ? undefined : formatRate(rate)}
                      className={cx(cellClass(a.key), 'border-b border-slate-200', fav === true && 'text-emerald-700', fav === false && 'text-red-600', fav === null && 'text-slate-300')}
                    >
                      {signed(a.diff)}
                      {a.key === 'total' && rate !== null && <span className="ml-1 text-xs font-normal">({formatRate(rate)})</span>}
                    </td>
                  )
                })}
              </tr>
            </tbody>
          )
        })}
      </table>
    </div>
  )
}
