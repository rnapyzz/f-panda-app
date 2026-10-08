import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { query } from '../../api/client'
import {
  milestoneStatusLabels,
  outlookLabels,
  unitTypeLabels,
  type List,
  type RiskActivity,
  type RiskReport,
  type Scenario,
  type Subject,
  type TreeNode,
  type Unit,
  type UnitType,
  type User,
} from '../../api/types'
import { Badge, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../../components/ui'
import { formatYen } from '../../lib/format'
import {
  compareDiffOf,
  compareProfit,
  compositionKeys,
  hasAmounts,
  highCertaintyRate,
  profitOf,
  sortActivities,
  spreadOf,
  sumComposition,
  sumMeasures,
  warningKinds,
  warningLabels,
  type SortKey,
  type WarningKind,
} from '../../lib/risk'
import { Link, navigate, useLocation } from '../../lib/router'
import { defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { buildTree, pathName } from '../../lib/tree'
import { useApi } from '../../lib/useApi'
import { RestrictedNote } from '../../components/RestrictedNote'

/** 見込の構成の色: 実績はグレー、段階は確度の高い順に濃い→薄い（1色相）、ダウンサイドは別の色相 */
const levelRamp = ['#312e81', '#4338ca', '#6366f1', '#a5b4fc', '#e0e7ff', '#eef2ff']
const actualColor = '#64748b'
const downsideColor = '#e11d48'

const signed = (v: bigint) => `${v > 0n ? '+' : ''}${formatYen(String(v))}`
const yen = (v: bigint) => formatYen(String(v))
const profitOfPL = (p: { revenue: string; expense: string }) => BigInt(p.revenue) - BigInt(p.expense)

/**
 * リスク画面（docs/plan.md「2.9」）。見込はどれくらい確かか・どこに振れ幅があるか・誰と話せばよいかに答える。
 * 楽観・基準（加重見込）・悲観は、確度の段階と見通しの種類から算出する（2.8）。
 */
export function RiskPage() {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const units = useApi<List<Unit>>('/units?include_archived=true')
  const segments = useApi<List<TreeNode>>('/segments')
  const subjects = useApi<List<Subject>>('/subjects')
  const users = useApi<List<User>>('/users')
  const segTree = useMemo(() => buildTree(segments.data?.items ?? []), [segments.data])
  const { search } = useLocation()
  const unitType = (['service', 'cost_center', 'corporate'] as const).find((t) => t === search.get('unit')) ?? ('' as UnitType | '')
  const period = search.get('period') === 'remaining' ? 'remaining' : 'year'

  const all = scenarios.data?.items ?? []
  const years = [...new Set(all.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const fy = Number(search.get('fy')) || all.find((s) => s.is_active)?.fiscal_year || years[0] || 0
  const inYear = all.filter((s) => s.fiscal_year === fy)
  // 基準の既定は作成中の版、なければ最新見込。比較の既定は基準の複製元（「なし」は 0）
  const base = Number(search.get('base')) || inYear.find((s) => s.is_active)?.id || defaultScenarios(inYear).latest?.id
  const baseScenario = inYear.find((s) => s.id === base)
  const cmp = search.has('cmp') ? Number(search.get('cmp')) || undefined : (baseScenario?.base_scenario_id ?? undefined)

  const update = (patch: Record<string, number | string | undefined>) => {
    navigate(`/risks${query({ fy, base, cmp: cmp ?? 0, unit: unitType, period, ...patch })}`, { replace: true })
  }
  const report = useApi<RiskReport>(base ? `/reports/risk${query({ scenario_id: base, compare_id: cmp, period })}` : null)

  const error = scenarios.error ?? units.error ?? segments.error ?? subjects.error ?? users.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !units.data || !segments.data || !subjects.data || !users.data) return <Loading />
  const unitById = new Map(units.data.items.map((u) => [u.id, u]))

  return (
    <>
      <PageHeader title="リスク" description="見込はどれくらい確かか、どこに振れ幅があるか、誰と話せばよいかを確認します。楽観・加重見込・悲観は、施策と内訳の確度の段階・見通しの種類から算出します。" />
      <RestrictedNote hidden={report.data?.restricted_hidden} className="mb-4" />
      <Card className="mb-4">
        <div className="grid gap-3 md:grid-cols-5">
          <Control label="年度">
            <Select aria-label="年度" value={fy} onChange={(e) => navigate(`/risks${query({ fy: e.target.value, unit: unitType, period })}`, { replace: true })}>
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          </Control>
          <Control label="シナリオ">
            <Select aria-label="シナリオ" value={base ?? ''} onChange={(e) => navigate(`/risks${query({ fy, base: e.target.value, unit: unitType, period })}`, { replace: true })}>
              <option value="">選択してください</option>
              {inYear.map((s) => (
                <option key={s.id} value={s.id}>
                  {scenarioLabel(s)}
                  {s.is_active ? '・今回の見込' : ''}
                </option>
              ))}
            </Select>
          </Control>
          <Control label="比較">
            <Select aria-label="比較" value={cmp ?? 0} onChange={(e) => update({ cmp: Number(e.target.value) })}>
              <option value={0}>なし</option>
              {inYear
                .filter((s) => s.id !== base)
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {scenarioLabel(s)}
                  </option>
                ))}
            </Select>
          </Control>
          <Control label="ユニットの種別">
            <Select aria-label="ユニットの種別" value={unitType} onChange={(e) => update({ unit: e.target.value })}>
              <option value="">すべて</option>
              {Object.entries(unitTypeLabels).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}のみ
                </option>
              ))}
            </Select>
          </Control>
          <Control label="期間">
            <Select aria-label="期間" value={period} onChange={(e) => update({ period: e.target.value })}>
              <option value="year">通期（実績を含む）</option>
              <option value="remaining">残り期間のみ</option>
            </Select>
          </Control>
        </div>
      </Card>

      {!base ? (
        <Card>
          <Empty>シナリオを選んでください</Empty>
        </Card>
      ) : report.error ? (
        <ErrorMessage error={report.error} />
      ) : !report.data ? (
        <Loading />
      ) : (
        <RiskView
          report={report.data}
          activities={report.data.activities.filter((a) => hasAmounts(a) && (!unitType || unitById.get(a.unit_id)?.unit_type === unitType))}
          unitById={unitById}
          segmentPath={(id) => pathName(segTree, id)}
          subjectName={(id) => subjects.data!.items.find((s) => s.id === id)?.name ?? `科目 #${id}`}
          userName={(id) => users.data!.items.find((u) => u.id === id)?.name ?? '未設定'}
        />
      )}
    </>
  )
}

function RiskView({
  report,
  activities,
  unitById,
  segmentPath,
  subjectName,
  userName,
}: {
  report: RiskReport
  activities: RiskActivity[]
  unitById: Map<number, Unit>
  segmentPath: (segmentId: number) => string
  subjectName: (id: number) => string
  userName: (id: number) => string
}) {
  const keys = compositionKeys(report.levels)
  return (
    <div className="space-y-4">
      <SummaryCard report={report} activities={activities} keys={keys} />
      <CompositionCard report={report} activities={activities} keys={keys} unitById={unitById} segmentPath={segmentPath} />
      <ActivitiesCard report={report} activities={activities} unitById={unitById} subjectName={subjectName} userName={userName} />
      <WarningsCard report={report} activities={activities} />
    </div>
  )
}

// --- 1. サマリー ---

function SummaryCard({ report, activities, keys }: { report: RiskReport; activities: RiskActivity[]; keys: string[] }) {
  const t = sumMeasures(activities)
  const pes = profitOf(t.pessimistic)
  const wgt = profitOf(t.weighted)
  const opt = profitOf(t.optimistic)
  const act = profitOf(t.actual)
  const rate = highCertaintyRate(sumComposition(activities, keys), report.levels)
  const warned = activities.filter((a) => a.warning_count > 0)
  const bad = warned.filter((a) => a.bad_for_level)
  const cmp = compareProfit(activities)
  return (
    <Card title={`サマリー（利益）・${report.period === 'year' ? '通期' : '残り期間'}`}>
      <ProfitBand pessimistic={pes} weighted={wgt} optimistic={opt} actual={report.period === 'year' ? act : null} />
      <dl className="mt-4 grid gap-3 sm:grid-cols-4">
        <Stat label="振れ幅（楽観 − 悲観）" value={yen(opt - pes)} />
        <Stat label="確度の高い見込の割合（売上）" value={rate === null ? '—' : `${rate}%`} hint="実績と確度の高い段階の売上 ÷ 満額の売上" />
        <Stat label="警告のある施策" value={`${warned.length} 件`} hint={bad.length > 0 ? `うち段階に対して状況が悪い ${bad.length} 件` : undefined} tone={bad.length > 0 ? 'red' : undefined} />
        <Stat label="比較との差（加重見込）" value={cmp === null ? '—' : signed(wgt - cmp)} hint={report.compare ? `比較: ${report.compare.name}` : '比較シナリオなし'} />
      </dl>
    </Card>
  )
}

/** 悲観 ― 加重見込 ― 楽観 を1本の帯で表す。実績で確定した分は濃いグレーで示す */
function ProfitBand({ pessimistic, weighted, optimistic, actual }: { pessimistic: bigint; weighted: bigint; optimistic: bigint; actual: bigint | null }) {
  const values = [pessimistic, weighted, optimistic, 0n, ...(actual === null ? [] : [actual])]
  const min = values.reduce((a, b) => (b < a ? b : a))
  const max = values.reduce((a, b) => (b > a ? b : a))
  const span = max - min || 1n
  const pos = (v: bigint) => Number(((v - min) * 1000n) / span) / 10
  // 加重見込のラベルは帯の上、悲観・楽観は帯の下に出す。端に近いラベルは内側に寄せる。
  // 悲観と楽観が近いときは、2つのラベルを1つにまとめて重ならないようにする
  const near = pos(optimistic) - pos(pessimistic) < 16
  const edge = (p: number): 'center' | 'left' | 'right' => (p > 80 ? 'right' : p < 20 ? 'left' : 'center')
  const rangeEdge = edge((pos(pessimistic) + pos(optimistic)) / 2)
  const rangeAt = rangeEdge === 'right' ? pos(optimistic) : rangeEdge === 'left' ? pos(pessimistic) : (pos(pessimistic) + pos(optimistic)) / 2
  const marker = (v: bigint, label: string, opts: { strong?: boolean; above?: boolean; hideLabel?: boolean } = {}) => {
    const { strong = false, above = false, hideLabel = false } = opts
    const align = edge(pos(v))
    return (
      <>
        <div className={cx('absolute top-6 h-7 w-0.5 -translate-x-1/2', strong ? 'bg-slate-900' : 'bg-slate-500')} style={{ left: `${pos(v)}%` }} />
        {!hideLabel && (
        <div
          className={cx(
            'absolute text-xs whitespace-nowrap',
            above ? 'top-0' : 'top-14',
            align === 'center' && '-translate-x-1/2 text-center',
            align === 'right' && '-translate-x-full text-right',
            align === 'left' && 'text-left',
          )}
          style={{ left: `${pos(v)}%` }}
        >
          {above ? (
            <span className={cx(strong ? 'font-semibold text-slate-900' : 'text-slate-700')}>
              {label} <span className="tabular-nums">{yen(v)}</span>
            </span>
          ) : (
            <>
              <div className="text-slate-500">{label}</div>
              <div className="text-slate-700 tabular-nums">{yen(v)}</div>
            </>
          )}
        </div>
        )}
      </>
    )
  }
  return (
    <div role="img" aria-label={`利益: 悲観 ${yen(pessimistic)}、加重見込 ${yen(weighted)}、楽観 ${yen(optimistic)}${actual === null ? '' : `、うち実績 ${yen(actual)}`}`} className="px-10 pb-2">
      <div className="relative h-28">
        <div className="absolute top-8 right-0 left-0 h-3 rounded-full bg-slate-100" />
        <div
          className="absolute top-8 h-3 rounded-full bg-indigo-200"
          style={{ left: `${pos(pessimistic)}%`, width: `${Math.max(pos(optimistic) - pos(pessimistic), 0.5)}%` }}
          title={`振れ幅 ${yen(optimistic - pessimistic)}`}
        />
        {actual !== null && actual !== 0n && (
          <div
            className="absolute top-8 h-3 rounded-full bg-slate-500"
            style={{ left: `${Math.min(pos(0n), pos(actual))}%`, width: `${Math.abs(pos(actual) - pos(0n))}%` }}
            title={`実績で確定した分 ${yen(actual)}`}
          />
        )}
        {marker(pessimistic, '悲観', { hideLabel: near })}
        {marker(weighted, '加重見込', { strong: true, above: true })}
        {marker(optimistic, '楽観', { hideLabel: near })}
        {near && (
          <div
            className={cx(
              'absolute top-14 text-xs whitespace-nowrap text-slate-700',
              rangeEdge === 'center' && '-translate-x-1/2',
              rangeEdge === 'right' && '-translate-x-full',
            )}
            style={{ left: `${rangeAt}%` }}
          >
            <span className="text-slate-500">悲観</span> <span className="tabular-nums">{yen(pessimistic)}</span> 〜 <span className="text-slate-500">楽観</span>{' '}
            <span className="tabular-nums">{yen(optimistic)}</span>
          </div>
        )}
      </div>
      {actual !== null && actual !== 0n && (
        <p className="mt-1 text-xs text-slate-500">
          <span className="mr-1 inline-block size-2.5 rounded-full bg-slate-500 align-middle" />
          実績で確定した分 {yen(actual)}（振れ幅は残りの月から生じます）
        </p>
      )}
    </div>
  )
}

function Stat({ label, value, hint, tone }: { label: string; value: string; hint?: string; tone?: 'red' }) {
  return (
    <div className="rounded-md border border-slate-200 px-3 py-2">
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className={cx('text-lg font-semibold tabular-nums', tone === 'red' ? 'text-red-700' : 'text-slate-900')}>{value}</dd>
      {hint && <dd className={cx('text-xs', tone === 'red' ? 'text-red-700' : 'text-slate-500')}>{hint}</dd>}
    </div>
  )
}

// --- 2. 見込の構成 ---

function CompositionCard({
  report,
  activities,
  keys,
  unitById,
  segmentPath,
}: {
  report: RiskReport
  activities: RiskActivity[]
  keys: string[]
  unitById: Map<number, Unit>
  segmentPath: (segmentId: number) => string
}) {
  const [asTable, setAsTable] = useState(false)
  const colorOf = (k: string) => (k === 'actual' ? actualColor : k === 'downside' ? downsideColor : levelRamp[Math.min(keys.indexOf(k) - 1, levelRamp.length - 1)])
  const labelOf = (k: string) => (k === 'actual' ? '実績' : k === 'downside' ? 'ダウンサイド' : `${k} ${report.levels.find((l) => l.code === k)?.name ?? ''}`)
  const rows = useMemo(() => {
    const byUnit = new Map<number, RiskActivity[]>()
    for (const a of activities) byUnit.set(a.unit_id, [...(byUnit.get(a.unit_id) ?? []), a])
    return [...byUnit.entries()]
      .map(([id, items]) => ({ id, label: unitById.get(id)?.name ?? `ユニット #${id}`, path: segmentPath(unitById.get(id)?.segment_id ?? 0), comp: sumComposition(items, keys) }))
      .sort((a, b) => a.path.localeCompare(b.path) || a.label.localeCompare(b.label))
  }, [activities, unitById, segmentPath, keys])
  const total = sumComposition(activities, keys)
  const positive = (comp: Map<string, bigint>) => keys.filter((k) => k !== 'downside').reduce((s, k) => s + ((comp.get(k) ?? 0n) > 0n ? comp.get(k)! : 0n), 0n)
  const maxTotal = [total, ...rows.map((r) => r.comp)].reduce((m, c) => (positive(c) > m ? positive(c) : m), 1n)

  const bar = (comp: Map<string, bigint>, scale: bigint) => {
    const sum = positive(comp)
    return (
      <div className="flex h-4 overflow-hidden rounded" style={{ width: `${Number((sum * 1000n) / scale) / 10}%`, minWidth: sum > 0n ? 4 : 0 }}>
        {keys
          .filter((k) => k !== 'downside' && (comp.get(k) ?? 0n) > 0n)
          .map((k) => {
            const v = comp.get(k)!
            return (
              <div
                key={k}
                className="h-full border-r-2 border-white last:border-r-0"
                style={{ width: `${Number((v * 1000n) / sum) / 10}%`, background: colorOf(k), outline: k === keys[keys.length - 2] ? '1px solid #c7d2fe' : undefined }}
                title={`${labelOf(k)}: ${yen(v)}（${Number((v * 1000n) / sum) / 10}%）`}
              />
            )
          })}
      </div>
    )
  }

  return (
    <Card
      title="見込の構成（売上・満額）"
      actions={
        <label className="flex items-center gap-1.5 text-xs text-slate-600">
          <input type="checkbox" className="size-4 rounded border-slate-300" checked={asTable} onChange={(e) => setAsTable(e.target.checked)} />
          表で見る
        </label>
      }
    >
      <ul className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600" aria-label="凡例">
        {keys.map((k) => (
          <li key={k} className="flex items-center gap-1.5">
            <span className="inline-block size-3 rounded-sm border border-slate-200" style={{ background: colorOf(k) }} />
            {labelOf(k)}
          </li>
        ))}
      </ul>
      {asTable ? (
        <div className="-mx-4 overflow-x-auto px-4">
          <Table>
            <thead>
              <tr>
                <th>ユニット</th>
                {keys.map((k) => (
                  <th key={k} className="text-right">
                    {labelOf(k)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {[{ id: 0, label: '全体', comp: total }, ...rows].map((r) => (
                <tr key={r.id} className={r.id === 0 ? 'bg-slate-50 font-semibold' : undefined}>
                  <td>{r.label}</td>
                  {keys.map((k) => (
                    <td key={k} className="text-right tabular-nums">
                      {(r.comp.get(k) ?? 0n) === 0n ? <span className="text-slate-300">—</span> : yen(r.comp.get(k)!)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      ) : (
        <table className="w-full text-sm" aria-label="見込の構成">
          <tbody>
            {[{ id: 0, label: '全体', path: '', comp: total }, ...rows].map((r) => (
              <tr key={r.id} className={r.id === 0 ? 'font-semibold' : undefined}>
                <th scope="row" className="w-48 py-1 pr-3 text-left font-normal">
                  <div className={cx(r.id === 0 && 'font-semibold')}>{r.label}</div>
                  {r.path && <div className="text-[11px] text-slate-400">{r.path}</div>}
                </th>
                <td className="py-1">{bar(r.comp, maxTotal)}</td>
                <td className="w-32 py-1 pl-3 text-right text-xs whitespace-nowrap text-slate-600 tabular-nums">
                  {yen(positive(r.comp))}
                  {(r.comp.get('downside') ?? 0n) !== 0n && <div className="text-rose-700">ダウンサイド {yen(r.comp.get('downside')!)}</div>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}

// --- 3. 施策の一覧 ---

const sortLabels: Record<SortKey, string> = { spread: '振れ幅の大きい順', warnings: '警告の多い順', compare: '比較との差の大きい順', code: '施策コードの順' }

function ActivitiesCard({
  report,
  activities,
  unitById,
  subjectName,
  userName,
}: {
  report: RiskReport
  activities: RiskActivity[]
  unitById: Map<number, Unit>
  subjectName: (id: number) => string
  userName: (id: number) => string
}) {
  const [sort, setSort] = useState<SortKey>('spread')
  const [open, setOpen] = useState<Set<number>>(new Set())
  const items = sortActivities(activities, sort)
  const toggle = (id: number) => {
    const next = new Set(open)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setOpen(next)
  }
  return (
    <Card
      title="施策の一覧（利益）"
      actions={
        <div className="w-48">
          <Select aria-label="並べ替え" value={sort} onChange={(e) => setSort(e.target.value as SortKey)} className="py-1 text-xs">
            {Object.entries(sortLabels).map(([k, l]) => (
              <option key={k} value={k}>
                {l}
              </option>
            ))}
          </Select>
        </div>
      }
    >
      {items.length === 0 ? (
        <Empty>金額のある施策はありません</Empty>
      ) : (
        <div className="-mx-4 overflow-x-auto px-4">
          <Table>
            <thead>
              <tr>
                <th>施策</th>
                <th>段階</th>
                <th className="text-right">悲観</th>
                <th className="text-right">加重見込</th>
                <th className="text-right">楽観</th>
                <th className="text-right">振れ幅</th>
                <th className="text-right">比較との差</th>
                <th>警告</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => {
                const diff = compareDiffOf(a)
                const isOpen = open.has(a.id)
                return (
                  <Fragment key={a.id}>
                    <tr className={cx(a.bad_for_level && 'bg-red-50/60')}>
                      <td>
                        <span className="flex items-center gap-1">
                          <button type="button" onClick={() => toggle(a.id)} aria-expanded={isOpen} aria-label={isOpen ? `${a.name}を閉じる` : `${a.name}を開く`} className="w-5 rounded text-slate-400 hover:bg-slate-100">
                            {isOpen ? '▾' : '▸'}
                          </button>
                          <Link to={`/activities/${a.id}`} className="font-medium text-indigo-700 hover:underline">
                            {a.name}
                          </Link>
                        </span>
                        <div className="pl-6 text-xs text-slate-400">
                          <span className="font-mono">{a.code}</span> ・{unitById.get(a.unit_id)?.name}
                        </div>
                      </td>
                      <td>
                        <Badge tone="slate">{a.confidence_level}</Badge>
                      </td>
                      <td className="text-right tabular-nums">{yen(profitOfPL(a.pessimistic))}</td>
                      <td className="text-right font-medium tabular-nums">{yen(profitOfPL(a.weighted))}</td>
                      <td className="text-right tabular-nums">{yen(profitOfPL(a.optimistic))}</td>
                      <td className="text-right tabular-nums">{yen(spreadOf(a))}</td>
                      <td className={cx('text-right tabular-nums', diff !== null && diff < 0n && 'text-red-700')}>{diff === null ? <span className="text-slate-300">—</span> : signed(diff)}</td>
                      <td className="whitespace-nowrap">
                        {a.bad_for_level && <Badge tone="red">段階に対して状況が悪い</Badge>}
                        {a.warning_count > 0 && !a.bad_for_level && <Badge tone="amber">⚠ {a.warning_count}</Badge>}
                        {a.bad_for_level && <span className="ml-1 text-xs text-red-700">⚠ {a.warning_count}</span>}
                      </td>
                    </tr>
                    {isOpen && (
                      <tr>
                        <td colSpan={8} className="bg-slate-50">
                          <ActivityDetail a={a} report={report} subjectName={subjectName} userName={userName} />
                        </td>
                      </tr>
                    )}
                  </Fragment>
                )
              })}
            </tbody>
          </Table>
        </div>
      )}
    </Card>
  )
}

function ActivityDetail({ a, report, subjectName, userName }: { a: RiskActivity; report: RiskReport; subjectName: (id: number) => string; userName: (id: number) => string }) {
  const kinds = warningKinds(a)
  return (
    <div className="grid gap-4 py-2 text-sm lg:grid-cols-2">
      <div>
        <h4 className="mb-1 text-xs font-semibold text-slate-500">内訳（{report.period === 'year' ? '通期' : '残り期間'}の計画値・満額）</h4>
        {a.lines.length === 0 ? (
          <p className="text-xs text-slate-400">内訳はありません</p>
        ) : (
          <table className="w-full text-xs">
            <tbody>
              {a.lines.map((l, i) => (
                <tr key={l.line_id ?? `direct-${l.subject_id}-${i}`} className="border-b border-slate-100">
                  <td className="py-1 pr-2 text-slate-500">{subjectName(l.subject_id)}</td>
                  <td className="py-1 pr-2">{l.name}</td>
                  <td className="py-1 pr-2">
                    <Badge tone="slate">{l.confidence_level}</Badge> {outlookLabels[l.outlook]}
                  </td>
                  <td className="py-1 text-right tabular-nums">{formatYen(l.amount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <p className="mt-2 text-xs text-slate-500">担当者: {a.owner_user_id ? userName(a.owner_user_id) : '未設定'}</p>
      </div>
      <div className="space-y-2">
        <div>
          <h4 className="mb-1 text-xs font-semibold text-slate-500">警告</h4>
          {kinds.length === 0 && a.warnings.milestones.length === 0 ? (
            <p className="text-xs text-slate-400">警告はありません</p>
          ) : (
            <ul className="space-y-0.5 text-xs">
              {a.warnings.milestones.map((m) => (
                <li key={m.name + m.due_date} className={m.risk === 'upcoming' ? 'text-slate-600' : 'text-red-700'}>
                  {m.risk === 'overdue' ? '⚠ 期日超過' : m.risk === 'delayed' ? '⚠ 遅延' : '期日が近い'}: {m.name}（{m.due_date}・{milestoneStatusLabels[m.status]}）
                </li>
              ))}
              {a.warnings.postponed.count > 0 && (
                <li className="text-red-700">
                  ⚠ 後ろ倒し: {a.warnings.postponed.count} 回・合計 {a.warnings.postponed.days} 日（比較シナリオの作成以降）
                </li>
              )}
              {a.warnings.downward && (
                <li className="text-red-700">
                  ⚠ 下方修正: {formatYen(a.warnings.downward.diff)}（−{a.warnings.downward.rate}%）
                </li>
              )}
              {a.warnings.consecutive && <li className="text-red-700">⚠ 連続の下方修正: 直近の見込で2回続けて下がっています</li>}
              {a.warnings.accuracy !== null && <li className="text-red-700">⚠ 見込の当たり具合: 直近の実績との差 {a.warnings.accuracy}%</li>}
            </ul>
          )}
        </div>
        <div>
          <h4 className="mb-1 text-xs font-semibold text-slate-500">前提条件・今回の見込の説明</h4>
          <p className="text-xs whitespace-pre-wrap text-slate-700">{a.assumptions || <span className="text-slate-400">前提条件は未入力</span>}</p>
          {a.conditions.scenario && <p className="mt-1 text-xs text-slate-700">今回の見込の説明: {a.conditions.scenario}</p>}
          {a.conditions.compare && <p className="mt-1 text-xs text-slate-700">比較シナリオの説明: {a.conditions.compare}</p>}
        </div>
      </div>
    </div>
  )
}

// --- 4. 警告の一覧 ---

function WarningsCard({ report, activities }: { report: RiskReport; activities: RiskActivity[] }) {
  const bad = activities.filter((a) => a.bad_for_level)
  const groups = (Object.keys(warningLabels) as WarningKind[]).map((k) => ({ kind: k, items: activities.filter((a) => warningKinds(a).includes(k)) }))
  const link = (a: RiskActivity) => (
    <Link key={a.id} to={`/activities/${a.id}?tab=update&scenario=${report.scenario.id}`} className="text-indigo-700 hover:underline">
      {a.name}
    </Link>
  )
  return (
    <Card title="警告の一覧">
      {bad.length > 0 && (
        <div className="mb-3 rounded-md border border-red-200 bg-red-50 p-3" aria-label="段階に対して状況が悪い施策">
          <p className="text-sm font-semibold text-red-800">段階に対して状況が悪い施策（確度の高い段階で、警告がある）</p>
          <p className="mt-1 flex flex-wrap gap-x-3 text-sm">{bad.map(link)}</p>
        </div>
      )}
      <dl className="space-y-2 text-sm">
        {groups.map((g) => (
          <div key={g.kind} className="flex flex-wrap gap-x-3">
            <dt className="w-56 shrink-0 text-slate-500">
              {warningLabels[g.kind]}（{g.items.length}）
            </dt>
            <dd className="flex flex-wrap gap-x-3">{g.items.length === 0 ? <span className="text-slate-300">—</span> : g.items.map(link)}</dd>
          </div>
        ))}
      </dl>
      {!report.compare && <p className="mt-2 text-xs text-slate-500">後ろ倒し・下方修正・当たり具合は、比較シナリオを選ぶと判定します。</p>}
    </Card>
  )
}

function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-slate-600">{label}</div>
      {children}
    </div>
  )
}
