import { useMemo, type ReactNode } from 'react'
import { query } from '../../api/client'
import { milestoneStatusLabels, unitTypeLabels, type Unit, type List, type PL, type RiskActivity, type RiskReport, type Scenario, type UnitType } from '../../api/types'
import { Badge, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table } from '../../components/ui'
import { formatPercent, formatYen } from '../../lib/format'
import { Link, navigate, useLocation } from '../../lib/router'
import { defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

const profit = (p: PL | null | undefined): bigint => (p ? BigInt(p.revenue) - BigInt(p.expense) : 0n)
/** 全体に占める割合（%、小数1桁）。全体が 0 なら null */
const share = (part: bigint, whole: bigint): number | null => (whole === 0n ? null : Number((part * 1000n) / whole) / 10)

/** 確度帯。上から順に判定する */
const bands = [
  { key: 'high', label: '90%以上', test: (p: number) => p >= 0.9 },
  { key: 'mid', label: '70〜90%', test: (p: number) => p >= 0.7 },
  { key: 'low', label: '50〜70%', test: (p: number) => p >= 0.5 },
  { key: 'verylow', label: '50%未満', test: () => true },
] as const
const LOW_PROBABILITY = 0.7

export function RiskPage() {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const units = useApi<List<Unit>>('/units')
  const { search } = useLocation()
  const unitType = (['service', 'cost_center', 'corporate'] as const).find((t) => t === search.get('unit')) ?? ('' as UnitType | '')

  const all = scenarios.data?.items ?? []
  const years = [...new Set(all.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const fy = Number(search.get('fy')) || years[0] || 0
  const inYear = all.filter((s) => s.fiscal_year === fy)
  const pick = (param: string, fallback: number | undefined) => (search.has(param) ? Number(search.get(param)) || undefined : fallback)
  // 基準の既定は最新見込。楽観・悲観として比べるシナリオは、一覧から自由に選ぶ
  const base = pick('base', defaultScenarios(inYear).latest?.id)
  const opt = pick('opt', undefined)
  const pes = pick('pes', undefined)

  const update = (patch: Record<string, number | string | undefined>) => {
    navigate(`/risks${query({ fy, base, opt: opt ?? '', pes: pes ?? '', unit: unitType, ...patch })}`, { replace: true })
  }

  const report = useApi<RiskReport>(base ? `/reports/risk${query({ scenario_id: base, optimistic_id: opt, pessimistic_id: pes })}` : null)

  if (scenarios.error ?? units.error) return <ErrorMessage error={scenarios.error ?? units.error} />
  if (!scenarios.data || !units.data) return <Loading />
  const unitOf = new Map(units.data.items.map((f) => [f.id, f.unit_type]))

  const options = (filter: (s: Scenario) => boolean) =>
    inYear.filter(filter).map((s) => (
      <option key={s.id} value={s.id}>
        {scenarioLabel(s)}
      </option>
    ))

  return (
    <>
      <PageHeader
        title="リスク"
        description="見込の確からしさを、確度・仮の値・楽観/悲観の振れ幅・マイルストーンの状況から確認します。"
      />
      <Card className="mb-4">
        <div className="grid gap-3 md:grid-cols-5">
          <Control label="年度">
            <Select value={fy} onChange={(e) => navigate(`/risks${query({ fy: e.target.value })}`, { replace: true })}>
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          </Control>
          <Control label="基準">
            <Select value={base ?? ''} onChange={(e) => update({ base: Number(e.target.value) || undefined })}>
              <option value="">選択してください</option>
              {options(() => true)}
            </Select>
          </Control>
          <Control label="楽観として比べる">
            <Select value={opt ?? ''} onChange={(e) => update({ opt: Number(e.target.value) || undefined })}>
              <option value="">（なし）</option>
              {options((s) => s.id !== base)}
            </Select>
          </Control>
          <Control label="悲観として比べる">
            <Select value={pes ?? ''} onChange={(e) => update({ pes: Number(e.target.value) || undefined })}>
              <option value="">（なし）</option>
              {options((s) => s.id !== base)}
            </Select>
          </Control>
          <Control label="ユニットの種別">
            <Select value={unitType} onChange={(e) => update({ unit: e.target.value })}>
              <option value="">すべて</option>
              {Object.entries(unitTypeLabels).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}のみ
                </option>
              ))}
            </Select>
          </Control>
        </div>
      </Card>

      {!base ? (
        <Card>
          <Empty>基準にするシナリオを選んでください</Empty>
        </Card>
      ) : report.error ? (
        <ErrorMessage error={report.error} />
      ) : !report.data ? (
        <Loading />
      ) : (
        <RiskView report={{ ...report.data, activities: report.data.activities.filter((a) => !unitType || unitOf.get(a.unit_id) === unitType) }} />
      )}
    </>
  )
}

function RiskView({ report }: { report: RiskReport }) {
  const acts = report.activities
  const hasRange = report.optimistic !== null || report.pessimistic !== null

  const totals = useMemo(() => {
    let revenue = 0n,
      expense = 0n,
      provisional = 0n,
      provisionalCount = 0,
      lowRevenue = 0n,
      lowCount = 0,
      baseProfit = 0n,
      optProfit = 0n,
      pesProfit = 0n
    const ms = { overdue: 0, delayed: 0, upcoming: 0 }
    for (const a of acts) {
      const r = BigInt(a.base.revenue)
      revenue += r
      expense += BigInt(a.base.expense)
      provisional += BigInt(a.provisional.revenue) + BigInt(a.provisional.expense)
      provisionalCount += a.provisional.count
      if (Number(a.confidence_rate) < LOW_PROBABILITY && r !== 0n) {
        lowRevenue += r
        lowCount++
      }
      baseProfit += profit(a.base)
      optProfit += profit(a.optimistic ?? a.base)
      pesProfit += profit(a.pessimistic ?? a.base)
      for (const m of a.milestones) ms[m.risk]++
    }
    return { revenue, expense, provisional, provisionalCount, lowRevenue, lowCount, baseProfit, optProfit, pesProfit, ms }
  }, [acts])

  return (
    <>
      <div className="mb-4 grid gap-3 md:grid-cols-4">
        <Tile
          label={`利益（${report.base.name}）`}
          value={`${formatYen(String(totals.baseProfit))} 円`}
          note={hasRange ? `悲観 ${formatYen(String(totals.pesProfit))} 〜 楽観 ${formatYen(String(totals.optProfit))}` : '楽観・悲観シナリオを選ぶと振れ幅を表示します'}
        />
        <Tile
          label="仮の値を含む金額"
          value={`${formatYen(String(totals.provisional))} 円`}
          note={`${totals.provisionalCount} 件・収益と費用の ${fmtShare(share(totals.provisional, totals.revenue + totals.expense))}`}
        />
        <Tile
          label={`確度 ${LOW_PROBABILITY * 100}% 未満の施策の売上`}
          value={`${formatYen(String(totals.lowRevenue))} 円`}
          note={`${totals.lowCount} 施策・売上の ${fmtShare(share(totals.lowRevenue, totals.revenue))}`}
        />
        <Tile
          label="注意が必要なマイルストーン"
          value={`${totals.ms.overdue + totals.ms.delayed + totals.ms.upcoming} 件`}
          note={`期日超過 ${totals.ms.overdue}・遅延 ${totals.ms.delayed}・30日以内 ${totals.ms.upcoming}`}
        />
      </div>

      {hasRange && <RangeSection report={report} />}
      <ProbabilitySection report={report} totalRevenue={totals.revenue} />
      <ProvisionalSection report={report} />
      <MilestoneSection report={report} />
    </>
  )
}

// --- 楽観・悲観の振れ幅 ---

function RangeSection({ report }: { report: RiskReport }) {
  const rows = report.activities
    .map((a) => {
      const b = profit(a.base)
      const o = profit(a.optimistic ?? a.base)
      const p = profit(a.pessimistic ?? a.base)
      const lo = [b, o, p].reduce((x, y) => (y < x ? y : x))
      const hi = [b, o, p].reduce((x, y) => (y > x ? y : x))
      return { a, b, o, p, lo, hi, range: hi - lo }
    })
    .filter((r) => r.range !== 0n || r.b !== 0n)
    .sort((x, y) => (y.range > x.range ? 1 : y.range < x.range ? -1 : 0))

  // 全施策で共通の目盛り（0 を含む）
  const min = rows.reduce((m, r) => (r.lo < m ? r.lo : m), 0n)
  const max = rows.reduce((m, r) => (r.hi > m ? r.hi : m), 0n)
  const span = max - min || 1n
  const pos = (v: bigint) => Number(((v - min) * 1000n) / span) / 10

  return (
    <Card title="楽観・悲観の振れ幅（利益・年間）" className="mb-4">
      <p className="mb-3 text-xs text-slate-500">
        振れ幅（楽観と悲観の差）が大きい順。棒は悲観〜楽観、● は基準（{report.base.name}）です。
      </p>
      {rows.length === 0 ? (
        <Empty>金額のある施策がありません</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="min-w-48">施策</th>
              <th className="text-right">悲観</th>
              <th className="text-right">基準</th>
              <th className="text-right">楽観</th>
              <th className="text-right">振れ幅</th>
              <th className="w-56 min-w-40">分布</th>
              <th className="min-w-56">想定条件</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(({ a, b, o, p, lo, hi, range }) => (
              <tr key={a.id} className="align-top">
                <td>
                  <ActivityLink a={a} scenarioId={report.base.id} />
                </td>
                <td className="text-right tabular-nums">{report.pessimistic ? formatYen(String(p)) : '—'}</td>
                <td className="text-right font-medium tabular-nums">{formatYen(String(b))}</td>
                <td className="text-right tabular-nums">{report.optimistic ? formatYen(String(o)) : '—'}</td>
                <td className="text-right tabular-nums">{formatYen(String(range))}</td>
                <td>
                  <div
                    className="relative h-5"
                    role="img"
                    aria-label={`悲観 ${formatYen(String(p))}、基準 ${formatYen(String(b))}、楽観 ${formatYen(String(o))}`}
                    title={`悲観 ${formatYen(String(p))} / 基準 ${formatYen(String(b))} / 楽観 ${formatYen(String(o))}`}
                  >
                    <div className="absolute top-1/2 h-px w-full bg-slate-200" />
                    {min < 0n && <div className="absolute top-0 h-5 w-px bg-slate-300" style={{ left: `${pos(0n)}%` }} />}
                    <div className="absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-slate-300" style={{ left: `${pos(lo)}%`, width: `${Math.max(pos(hi) - pos(lo), 0.5)}%` }} />
                    <div className="absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-indigo-600 ring-2 ring-white" style={{ left: `${pos(b)}%` }} />
                  </div>
                </td>
                <td className="max-w-72 text-xs text-slate-600">
                  {a.conditions.optimistic && <Condition label="楽観" text={a.conditions.optimistic} />}
                  {a.conditions.pessimistic && <Condition label="悲観" text={a.conditions.pessimistic} />}
                  {!a.conditions.optimistic && !a.conditions.pessimistic && (range !== 0n ? <Badge tone="amber">想定条件の記録なし</Badge> : <span className="text-slate-400">—</span>)}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  )
}

function Condition({ label, text }: { label: string; text: string }) {
  return (
    <p className="mb-1">
      <span className="mr-1 font-medium text-slate-500">{label}:</span>
      <span className="whitespace-pre-wrap">{text}</span>
    </p>
  )
}

// --- 確度 ---

function ProbabilitySection({ report, totalRevenue }: { report: RiskReport; totalRevenue: bigint }) {
  // 確度帯＋「未設定」の集計。確度がない施策は最後の「未設定」に入れる
  const buckets = [...bands.map((b) => ({ key: b.key, label: b.label, revenue: 0n, count: 0 })), { key: 'none', label: '未設定', revenue: 0n, count: 0 }]
  for (const a of report.activities) {
    const r = BigInt(a.base.revenue)
    if (r === 0n) continue
    const i = bands.findIndex((b) => b.test(Number(a.confidence_rate)))
    buckets[i].revenue += r
    buckets[i].count++
  }
  const maxRevenue = buckets.reduce((m, b) => (b.revenue > m ? b.revenue : m), 0n) || 1n
  const low = report.activities
    .filter((a) => Number(a.confidence_rate) < LOW_PROBABILITY && BigInt(a.base.revenue) !== 0n)
    .sort((x, y) => (BigInt(y.base.revenue) > BigInt(x.base.revenue) ? 1 : -1))

  return (
    <Card title="確度別の売上（基準・年間）" className="mb-4">
      <div className="grid gap-6 lg:grid-cols-2">
        <Table>
          <thead>
            <tr>
              <th>確度</th>
              <th className="text-right">施策数</th>
              <th className="text-right">売上</th>
              <th className="text-right">構成比</th>
              <th className="w-40" />
            </tr>
          </thead>
          <tbody>
            {buckets.map((b) => (
              <tr key={b.key}>
                <td>{b.label}</td>
                <td className="text-right tabular-nums">{b.count}</td>
                <td className="text-right tabular-nums">{formatYen(String(b.revenue))}</td>
                <td className="text-right tabular-nums">{fmtShare(share(b.revenue, totalRevenue))}</td>
                <td>
                  <div className="h-2 rounded-full bg-slate-100" title={`${b.label}: ${formatYen(String(b.revenue))} 円`}>
                    <div className="h-2 rounded-full bg-indigo-500" style={{ width: `${Number((b.revenue * 1000n) / maxRevenue) / 10}%` }} />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
        <div>
          <h3 className="mb-2 text-xs font-semibold text-slate-500">確度 {LOW_PROBABILITY * 100}% 未満の施策（売上の大きい順）</h3>
          {low.length === 0 ? (
            <p className="text-sm text-slate-500">該当する施策はありません</p>
          ) : (
            <ul className="divide-y divide-slate-100">
              {low.map((a) => (
                <li key={a.id} className="py-2 text-sm">
                  <div className="flex items-baseline justify-between gap-3">
                    <ActivityLink a={a} scenarioId={report.base.id} />
                    <span className="shrink-0 tabular-nums">
                      {formatYen(a.base.revenue)} 円<span className="ml-2 text-xs text-slate-500">確度 {a.confidence_level}（{formatPercent(Number(a.confidence_rate))}）</span>
                    </span>
                  </div>
                  {a.assumptions && <p className="mt-0.5 line-clamp-2 text-xs whitespace-pre-wrap text-slate-600">前提: {a.assumptions}</p>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Card>
  )
}

// --- 仮の値 ---

function ProvisionalSection({ report }: { report: RiskReport }) {
  const rows = report.activities.filter((a) => a.provisional.count > 0)
  return (
    <Card title={`仮の値（${report.base.name}）`} className="mb-4">
      {rows.length === 0 ? (
        <Empty>仮の値はありません</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="min-w-48">施策</th>
              <th className="text-right">件数</th>
              <th className="text-right">収益</th>
              <th className="text-right">費用</th>
              <th>理由</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => (
              <tr key={a.id} className="align-top">
                <td>
                  <ActivityLink a={a} scenarioId={report.base.id} />
                </td>
                <td className="text-right tabular-nums">{a.provisional.count}</td>
                <td className="text-right tabular-nums">{formatYen(a.provisional.revenue)}</td>
                <td className="text-right tabular-nums">{formatYen(a.provisional.expense)}</td>
                <td className="text-xs text-slate-600">{a.provisional.reasons.join(' / ') || '—'}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  )
}

// --- マイルストーン ---

const riskOrder = { overdue: 0, delayed: 1, upcoming: 2 } as const
const riskBadge = {
  overdue: <Badge tone="red">⚠ 期日超過</Badge>,
  delayed: <Badge tone="amber">遅延</Badge>,
  upcoming: <Badge>30日以内</Badge>,
}

function MilestoneSection({ report }: { report: RiskReport }) {
  const rows = report.activities
    .flatMap((a) => a.milestones.map((m) => ({ a, m })))
    .sort((x, y) => riskOrder[x.m.risk] - riskOrder[y.m.risk] || x.m.due_date.localeCompare(y.m.due_date))
  return (
    <Card title={`注意が必要なマイルストーン（${report.today} 時点）`}>
      {rows.length === 0 ? (
        <Empty>期日超過・遅延・期日が近いマイルストーンはありません</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="w-28">区分</th>
              <th className="w-28">期日</th>
              <th>マイルストーン</th>
              <th>状態</th>
              <th>施策</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(({ a, m }, i) => (
              <tr key={i}>
                <td>{riskBadge[m.risk]}</td>
                <td className="tabular-nums">{m.due_date}</td>
                <td>{m.name}</td>
                <td className="text-slate-600">{milestoneStatusLabels[m.status]}</td>
                <td>
                  <Link to={`/activities/${a.id}`} className="text-indigo-700 hover:underline">
                    {a.name}
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </Card>
  )
}

// --- 補助 ---

function ActivityLink({ a, scenarioId }: { a: RiskActivity; scenarioId: number }) {
  return (
    <span>
      <Link to={`/scenarios/${scenarioId}/activities/${a.id}`} className="font-medium text-indigo-700 hover:underline">
        {a.name}
      </Link>
      <span className="ml-1 font-mono text-xs text-slate-400">{a.code}</span>
    </span>
  )
}

function Tile({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 shadow-xs">
      <div className="text-xs font-medium text-slate-500">{label}</div>
      <div className="mt-1 text-xl font-bold text-slate-900 tabular-nums">{value}</div>
      <div className="mt-1 text-xs text-slate-500">{note}</div>
    </div>
  )
}

function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-xs font-medium text-slate-500">
      {label}
      <div className="mt-1">{children}</div>
    </label>
  )
}

function fmtShare(v: number | null): string {
  return v === null ? '—' : `${v}%`
}
