import { useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { query } from '../../api/client'
import { activityTypeLabels, type Activity, type ActivityType, type List, type RiskActivity, type RiskReport, type Scenario, type TreeNode, type Unit } from '../../api/types'
import { RestrictedNote } from '../../components/RestrictedNote'
import { SaveImageButton } from '../../components/SaveImageButton'
import { Card, Empty, ErrorMessage, Loading, cx } from '../../components/ui'
import {
  bubblePoints,
  chartViewLabels,
  diffBucket,
  niceTicks,
  pipelineByMonth,
  profit,
  rangeRows,
  squarify,
  waterfallSteps,
  yearPosition,
  type ChartView,
  type DiffBucket,
  type Rect,
} from '../../lib/activityCharts'
import { actualColor, diverging, downColor, downsideColor, ink, levelRamp, totalColor, typeColor, upColor } from '../../lib/chartTheme'
import { formatYen, monthLabel } from '../../lib/format'
import { navigate } from '../../lib/router'
import { currentFiscalYear, defaultScenarios, fiscalMonths, scenarioLabel, todayInTokyo } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

const diffFill: Record<DiffBucket, string> = { [-2]: diverging.strongDown, [-1]: diverging.down, 0: diverging.mid, 1: diverging.up, 2: diverging.strongUp }
const diffLabels: Record<DiffBucket, string> = { [-2]: '−20% 以下', [-1]: '−20〜−5%', 0: '±5% 未満', 1: '+5〜+20%', 2: '+20% 以上' }

/** 金額を「1,234万」の形にする（図の目盛り・ラベル用） */
function man(n: number, signed = false): string {
  const v = Math.round(n / 10_000)
  return `${signed && v > 0 ? '+' : ''}${formatYen(v)}万`
}
const yen = (n: number) => `${formatYen(Math.round(n))} 円`
const signedYen = (n: number) => `${n > 0 ? '+' : ''}${yen(n)}`

/**
 * 施策の一覧の「図で見る」（docs/plan.md「2.22」）。今回の見込（なければ今年度の最新見込）の年間の金額を、
 * 一覧の絞り込み（activities）に合わせて図にする。金額はリスク画面と同じ集計（GET /api/reports/risk）を使う。
 */
export function ActivityCharts({ view, activities }: { view: ChartView; activities: Activity[] }) {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const units = useApi<List<Unit>>('/units')
  const segments = useApi<List<TreeNode>>('/segments')
  const [compareTo, setCompareTo] = useState<'target' | 'previous'>('target')
  const captureRef = useRef<HTMLDivElement>(null)
  const list = scenarios.data?.items ?? []
  const scenario = list.find((s) => s.is_active) ?? defaultScenarios(list.filter((s) => s.fiscal_year === currentFiscalYear())).latest ?? list[0]
  const target = scenario ? defaultScenarios(list.filter((s) => s.fiscal_year === scenario.fiscal_year)).base : undefined
  const previous = scenario?.previous_scenario_id ? list.find((s) => s.id === scenario.previous_scenario_id) : undefined
  // 比較: ツリーマップは目標、増減の内訳は目標か前回の見込
  const compare = view === 'waterfall' && compareTo === 'previous' ? previous : target
  const compareId = compare && compare.id !== scenario?.id ? compare.id : undefined
  const report = useApi<RiskReport>(scenario ? `/reports/risk${query({ scenario_id: scenario.id, compare_id: compareId })}` : null)

  const error = scenarios.error ?? units.error ?? segments.error ?? report.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !units.data || !segments.data) return <Loading />
  if (!scenario) return <Empty>シナリオがまだありません。</Empty>
  if (!report.data) return <Loading />

  const ids = new Set(activities.map((a) => a.id))
  const items = report.data.activities.filter((a) => ids.has(a.id))
  const unitById = new Map(units.data.items.map((u) => [u.id, u]))
  const unitName = (id: number) => unitById.get(id)?.name ?? ''
  const segmentName = new Map(segments.data.items.map((s) => [s.id, s.name]))

  let chart: ReactNode
  if (items.length === 0 && view !== 'timeline') chart = <Empty>条件に合う施策がありません</Empty>
  else if (view === 'portfolio') chart = <PortfolioMap items={items} unitName={unitName} levels={report.data.levels} />
  else if (view === 'treemap')
    chart = <Treemap items={items} unitOf={(id) => unitById.get(id)} segmentName={(id) => segmentName.get(id) ?? ''} hasTarget={compareId !== undefined} />
  else if (view === 'range') chart = <RangeChart items={items} unitName={unitName} />
  else if (view === 'waterfall')
    chart = (
      <WaterfallChart
        items={items}
        startLabel={compareId ? (compareTo === 'previous' ? '前回の見込' : '目標') : '比較なし'}
        compareTo={compareTo}
        onCompareTo={setCompareTo}
        hasPrevious={previous !== undefined && previous.id !== scenario.id}
      />
    )
  else if (view === 'timeline') chart = <TimelineChart activities={activities} scenario={scenario} unitName={unitName} />
  else chart = <PipelineChart items={items} months={fiscalMonths(scenario.fiscal_year)} levels={report.data.levels} actualThrough={scenario.actual_through} />

  return (
    <div ref={captureRef} data-capture={chartViewLabels[view]} data-capture-file={`${chartViewLabels[view]}_${scenario.name}`} className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-slate-500">
          {scenarioLabel(scenario)}（{scenario.fiscal_year}年度・年間、実績の月は実績）
          {(view === 'treemap' || view === 'waterfall') && (
            <>
              {' '}
              ／ 比較: {compareId && compare ? scenarioLabel(compare) : 'なし'}
            </>
          )}
        </p>
        {(items.length > 0 || view === 'timeline') && <SaveImageButton target={captureRef} fileName={`${chartViewLabels[view]}_${scenario.name}`} title={chartViewLabels[view]} />}
      </div>
      <RestrictedNote hidden={report.data.restricted_hidden} />
      {chart}
    </div>
  )
}

// --- 共通: カード・凡例・ツールチップ ---

function ChartCard({ legend, actions, footer, children }: { legend?: ReactNode; actions?: ReactNode; footer?: ReactNode; children: ReactNode }) {
  return (
    <Card>
      {(legend || actions) && (
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-slate-600" aria-label="凡例">
            {legend}
          </div>
          {actions && <div data-no-capture>{actions}</div>}
        </div>
      )}
      {children}
      {footer && <p className="mt-3 text-xs text-slate-500">{footer}</p>}
    </Card>
  )
}

function Swatch({ color, label, shape = 'dot' }: { color: string; label: ReactNode; shape?: 'dot' | 'square' | 'line' }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className={cx('inline-block shrink-0', shape === 'dot' ? 'size-2.5 rounded-full' : shape === 'square' ? 'size-3 rounded-[3px]' : 'h-1 w-4 rounded-full')}
        style={{ background: color }}
      />
      {label}
    </span>
  )
}

function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div role="group" aria-label={label} className="inline-flex rounded-full bg-slate-100 p-0.5">
      {options.map(([v, l]) => (
        <button
          key={v}
          type="button"
          aria-pressed={value === v}
          onClick={() => onChange(v)}
          className={cx('rounded-full px-3 py-1 text-xs font-medium transition-colors', value === v ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800')}
        >
          {l}
        </button>
      ))}
    </div>
  )
}

type Tip = { x: number; y: number; content: ReactNode; flip: boolean }

/** 図の上に重ねるツールチップ。位置は図の枠からの px */
function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  return (
    <div
      role="tooltip"
      data-no-capture
      className="pointer-events-none absolute z-20 w-64 rounded-lg bg-slate-900/95 px-3 py-2.5 text-xs text-slate-100 shadow-xl ring-1 ring-black/5"
      // 右端に近いときは、カーソルの左に出して枠からはみ出さないようにする（幅は w-64 = 256px）
      style={{ left: tip.flip ? Math.max(4, tip.x - 14 - 256) : tip.x + 14, top: tip.y + 14 }}
    >
      {tip.content}
    </div>
  )
}

/** マウス・キーボードの両方で、図の印にツールチップを出し、クリック・Enter で施策を開く（id が null なら開かない） */
function useMarks() {
  const [tip, setTip] = useState<Tip | null>(null)
  const [active, setActive] = useState<string | null>(null)
  const at = (e: MouseEvent | FocusEvent, key: string, content: ReactNode) => {
    const box = (e.currentTarget as Element).closest('[data-chart]')!.getBoundingClientRect()
    const r = (e.currentTarget as Element).getBoundingClientRect()
    const p = 'clientX' in e ? { x: e.clientX, y: e.clientY } : { x: r.left + r.width / 2, y: r.top + r.height / 2 }
    setActive(key)
    setTip({ x: p.x - box.left, y: p.y - box.top, content, flip: p.x - box.left + 14 + 256 > box.width })
  }
  const props = (key: string | number, id: number | null, label: string, content: ReactNode) => {
    const k = String(key)
    const open = () => id !== null && navigate(`/activities/${id}`)
    return {
      tabIndex: 0,
      role: id !== null ? 'link' : 'img',
      'aria-label': label,
      onMouseEnter: (e: MouseEvent) => at(e, k, content),
      onMouseMove: (e: MouseEvent) => at(e, k, content),
      onFocus: (e: FocusEvent) => at(e, k, content),
      onMouseLeave: () => (setTip(null), setActive(null)),
      onBlur: () => (setTip(null), setActive(null)),
      onClick: open,
      onKeyDown: (e: KeyboardEvent) => e.key === 'Enter' && open(),
      style: { cursor: id !== null ? 'pointer' : 'default', outline: 'none' },
    }
  }
  return { tip, active: (key: string | number) => active === String(key), anyActive: active !== null, props }
}

function TipBody({ title, sub, rows, link = true }: { title: string; sub?: string; rows: [string, string][]; link?: boolean }) {
  return (
    <>
      <div className="font-semibold text-white">{title}</div>
      {sub && <div className="mb-1.5 text-slate-400">{sub}</div>}
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-slate-400">{k}</dt>
            <dd className="text-right tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      {link && <div className="mt-1.5 text-[11px] text-slate-500">クリックで施策を開く</div>}
    </>
  )
}

const subOf = (a: { code: string; activity_type: ActivityType }, unit: string) => `${a.code} ・ ${unit} ・ ${activityTypeLabels[a.activity_type]}`

/** 文字の幅の概算（全角は fontSize、半角は fontSize の 0.6 倍） */
function textWidth(text: string, fontSize: number): number {
  let w = 0
  for (const ch of text) w += ch.charCodeAt(0) > 0xff ? fontSize : fontSize * 0.6
  return w
}

/** 幅に収まらない文字は省略する */
function clip(text: string, width: number, fontSize: number): string {
  if (textWidth(text, fontSize) <= width) return text
  let out = ''
  for (const ch of text) {
    if (textWidth(`${out}${ch}…`, fontSize) > width) break
    out += ch
  }
  return out ? `${out}…` : ''
}

const shortName = (name: string) => (name.length > 14 ? `${name.slice(0, 14)}…` : name)

// --- ポートフォリオ・マップ ---

const W = 960
const H = 480
const M = { top: 40, right: 24, bottom: 44, left: 84 }
// 丸が枠からはみ出さないよう、描く範囲の内側に取る余白（いちばん大きい丸の半径）
const PAD = 32

/** 横軸は確度の割合、縦軸は年間の利益、丸の大きさは売上、色は施策のタイプ */
function PortfolioMap({ items, unitName, levels }: { items: RiskActivity[]; unitName: (id: number) => string; levels: RiskReport['levels'] }) {
  const { tip, active, anyActive, props } = useMarks()
  const { points, skipped } = bubblePoints(items)
  if (points.length === 0) return <Empty>売上のある施策がありません（ポートフォリオは売上のある施策を表示します）</Empty>
  const ys = niceTicks(Math.min(0, ...points.map((p) => p.profit)), Math.max(0, ...points.map((p) => p.profit)))
  const [y0, y1] = [ys[0], ys[ys.length - 1]]
  const maxRevenue = Math.max(...points.map((p) => p.revenue))
  const px = (c: number) => M.left + PAD + c * (W - M.left - M.right - PAD * 2)
  const py = (v: number) => M.top + PAD + ((y1 - v) / (y1 - y0)) * (H - M.top - M.bottom - PAD * 2)
  const r = (rev: number) => 5 + 26 * Math.sqrt(rev / maxRevenue)
  // 直接のラベルは、売上の大きい順に5件まで（すべてには付けない）。ほかのラベルと重なるものは付けない
  const labelBox = (p: (typeof points)[number]) => {
    const w = textWidth(shortName(p.activity.name), 12)
    const anchor = p.certainty > 0.85 ? 'end' : p.certainty < 0.15 ? 'start' : 'middle'
    const x = px(p.certainty)
    const left = anchor === 'end' ? x - w : anchor === 'start' ? x : x - w / 2
    const y = py(p.profit) - r(p.revenue) - 7
    return { left, right: left + w, top: y - 13, bottom: y + 3 }
  }
  const placed: ReturnType<typeof labelBox>[] = []
  const labeled = new Set<number>()
  for (const p of points) {
    if (labeled.size >= 5) break
    const b = labelBox(p)
    if (placed.some((q) => b.left < q.right + 4 && b.right + 4 > q.left && b.top < q.bottom && b.bottom > q.top)) continue
    placed.push(b)
    labeled.add(p.activity.id)
  }
  const types = (Object.keys(typeColor) as ActivityType[]).filter((t) => points.some((p) => p.activity.activity_type === t))
  const quadrant = (x: number, y: number, text: string, anchor: 'start' | 'end') => (
    <text x={x} y={y} textAnchor={anchor} fontSize={12} fill={ink.muted}>
      {text}
    </text>
  )

  return (
    <ChartCard
      legend={
        <>
          {types.map((t) => (
            <Swatch key={t} color={typeColor[t]} label={activityTypeLabels[t]} />
          ))}
          <span className="inline-flex items-center gap-1.5 text-slate-500">
            <svg width="26" height="14" aria-hidden="true">
              <circle cx="5" cy="9" r="4" fill="none" stroke={ink.muted} />
              <circle cx="18" cy="7" r="7" fill="none" stroke={ink.muted} />
            </svg>
            丸の大きさ: 売上（満額）
          </span>
        </>
      }
      footer={skipped > 0 ? `売上のない施策（費用だけの施策など）${skipped} 件は表示していません。ツリーマップの「費用」で見られます。` : undefined}
    >
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="group" aria-label="ポートフォリオ・マップ（横軸: 確度の割合、縦軸: 年間の利益、丸の大きさ: 売上）">
          <defs>
            {/* 右上（確実で利益が大きい）をほのかに色づける */}
            <linearGradient id="pf-quadrant" x1="0" y1="1" x2="1" y2="0">
              <stop offset="0.55" stopColor={ink.accent} stopOpacity="0" />
              <stop offset="1" stopColor={ink.accent} stopOpacity="0.06" />
            </linearGradient>
          </defs>
          <rect x={M.left} y={M.top} width={W - M.left - M.right} height={H - M.top - M.bottom} rx={12} fill="url(#pf-quadrant)" />
          {ys.map((v) => (
            <g key={v}>
              <line x1={M.left} x2={W - M.right} y1={py(v)} y2={py(v)} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={M.left - 10} y={py(v)} textAnchor="end" dominantBaseline="middle" fontSize={12} fill={ink.secondary} className="tabular-nums">
                {man(v)}
              </text>
            </g>
          ))}
          {[0, 0.25, 0.5, 0.75, 1].map((c) => (
            <g key={c}>
              <line x1={px(c)} x2={px(c)} y1={M.top} y2={H - M.bottom} stroke={c === 0.5 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={px(c)} y={H - M.bottom + 20} textAnchor="middle" fontSize={12} fill={ink.secondary}>
                {c * 100}%
              </text>
            </g>
          ))}
          <text x={(M.left + W - M.right) / 2} y={H - 4} textAnchor="middle" fontSize={12} fill={ink.secondary}>
            確度の割合（加重見込 ÷ 満額、売上） →
          </text>
          <text x={16} y={(M.top + H - M.bottom) / 2} textAnchor="middle" fontSize={12} fill={ink.secondary} transform={`rotate(-90 16 ${(M.top + H - M.bottom) / 2})`}>
            年間の利益（満額） →
          </text>
          {quadrant(W - M.right, M.top - 14, '確実で利益が大きい ↗', 'end')}
          {quadrant(M.left, M.top - 14, '↖ 利益は大きいが不確か', 'start')}
          {y0 < 0 && quadrant(W - M.right - 8, H - M.bottom - 10, '確実だが赤字', 'end')}
          {y0 < 0 && quadrant(M.left + 8, H - M.bottom - 10, '不確かで赤字', 'start')}

          {/* 丸（大きい順に描く）。重なりは面の色の輪で分ける */}
          {points.map((p) => {
            const a = p.activity
            const level = levels.find((l) => l.code === a.confidence_level)
            const on = active(a.id)
            return (
              <circle
                key={a.id}
                cx={px(p.certainty)}
                cy={py(p.profit)}
                r={r(p.revenue) + (on ? 2 : 0)}
                fill={typeColor[a.activity_type]}
                fillOpacity={!anyActive || on ? 0.82 : 0.25}
                stroke={ink.surface}
                strokeWidth={2}
                {...props(a.id, a.id, `${a.name}: 確度の割合 ${Math.round(p.certainty * 100)}%、利益 ${yen(p.profit)}、売上 ${yen(p.revenue)}`, (
                  <TipBody
                    title={a.name}
                    sub={subOf(a, unitName(a.unit_id))}
                    rows={[
                      ['確度の割合', `${Math.round(p.certainty * 100)}%`],
                      ['施策の段階', level ? `${level.code} ${level.name}（${Math.round(Number(level.rate) * 100)}%）` : a.confidence_level],
                      ['売上（満額）', yen(p.revenue)],
                      ['利益（満額）', yen(p.profit)],
                      ['利益（加重見込）', yen(profit(a.weighted))],
                    ]}
                  />
                ))}
              />
            )
          })}
          {points
            .filter((p) => labeled.has(p.activity.id))
            .map((p) => (
              <text
                key={p.activity.id}
                x={px(p.certainty)}
                y={py(p.profit) - r(p.revenue) - 7}
                textAnchor={p.certainty > 0.85 ? 'end' : p.certainty < 0.15 ? 'start' : 'middle'}
                fontSize={12}
                fontWeight={500}
                fill={ink.primary}
                pointerEvents="none"
              >
                {shortName(p.activity.name)}
              </text>
            ))}
        </svg>
        <Tooltip tip={tip} />
      </div>
    </ChartCard>
  )
}

// --- ツリーマップ ---

type Metric = 'revenue' | 'expense'
const TW = 960
const TH = 520

/** セグメント → ユニット → 施策の入れ子。面積は売上（または費用）、色は目標との差（加重見込の利益） */
function Treemap({ items, unitOf, segmentName, hasTarget }: { items: RiskActivity[]; unitOf: (id: number) => Unit | undefined; segmentName: (id: number) => string; hasTarget: boolean }) {
  const [metric, setMetric] = useState<Metric>('revenue')
  const { tip, active, props } = useMarks()
  const value = (a: RiskActivity) => Number(BigInt(a.full[metric]))

  const bySegment = new Map<number, Map<number, RiskActivity[]>>()
  for (const a of items) {
    if (value(a) <= 0) continue
    const seg = unitOf(a.unit_id)?.segment_id ?? 0
    const units = bySegment.get(seg) ?? new Map<number, RiskActivity[]>()
    units.set(a.unit_id, [...(units.get(a.unit_id) ?? []), a])
    bySegment.set(seg, units)
  }
  const sum = (list: RiskActivity[]) => list.reduce((s, a) => s + value(a), 0)
  const segs = squarify(
    [...bySegment].map(([id, units]) => ({ id, units, value: [...units.values()].reduce((s, l) => s + sum(l), 0) })),
    { x: 0, y: 0, w: TW, h: TH },
  )
  const inset = (r: Rect, top: number, pad = 3): Rect => ({ x: r.x + pad, y: r.y + top, w: Math.max(0, r.w - pad * 2), h: Math.max(0, r.h - top - pad) })

  return (
    <ChartCard
      legend={
        <>
          <span className="text-slate-500">目標との差（加重見込の利益）</span>
          {([-2, -1, 0, 1, 2] as DiffBucket[]).map((b) => (
            <Swatch key={b} shape="square" color={diffFill[b]} label={diffLabels[b]} />
          ))}
          <Swatch shape="square" color={diverging.none} label="目標なし" />
        </>
      }
      actions={
        <Segmented
          label="面積"
          value={metric}
          options={[
            ['revenue', '面積: 売上'],
            ['expense', '面積: 費用'],
          ]}
          onChange={setMetric}
        />
      }
      footer="色は、年間の利益（加重見込）の目標との差の率です。面積が小さく名前を出せない施策は、カーソルを当てると詳細が出ます。"
    >
      {segs.length === 0 ? (
        <Empty>{metric === 'revenue' ? '売上' : '費用'}のある施策がありません</Empty>
      ) : (
        <div className="relative" data-chart>
          <svg viewBox={`0 0 ${TW} ${TH}`} className="h-auto w-full" role="group" aria-label={`ツリーマップ（面積: ${metric === 'revenue' ? '売上' : '費用'}、色: 目標との差）`}>
            {segs.map((s) => {
              const segBox = inset(s.rect, 22)
              const units = squarify(
                [...s.units].map(([id, list]) => ({ id, list, value: sum(list) })),
                segBox,
              )
              return (
                <g key={s.id}>
                  <rect x={s.rect.x + 2} y={s.rect.y + 2} width={Math.max(0, s.rect.w - 4)} height={Math.max(0, s.rect.h - 4)} rx={10} fill="#f6f7f9" />
                  {s.rect.w > 60 && (
                    <text x={s.rect.x + 10} y={s.rect.y + 16} fontSize={11} fontWeight={600} fill={ink.secondary}>
                      {clip(segmentName(s.id) || '（セグメントなし）', s.rect.w - 20, 11)}
                    </text>
                  )}
                  {units.map((u) => {
                    const unitBox = inset(u.rect, 16, 1)
                    const acts = squarify(
                      u.list.map((a) => ({ id: a.id, a, value: value(a) })),
                      unitBox,
                    )
                    return (
                      <g key={u.id}>
                        {u.rect.w > 50 && (
                          <text x={u.rect.x + 5} y={u.rect.y + 11} fontSize={10} fill={ink.muted}>
                            {clip(unitOf(u.id)?.name ?? '', u.rect.w - 10, 10)}
                          </text>
                        )}
                        {acts.map(({ a, rect }) => {
                          const current = profit(a.weighted)
                          const target = hasTarget && a.compare ? profit(a.compare) : null
                          const bucket = diffBucket(current, target)
                          const fill = bucket === null ? diverging.none : diffFill[bucket]
                          const dark = bucket === 2 || bucket === -2
                          const diff = target === null ? null : current - target
                          return (
                            <g
                              key={a.id}
                              {...props(a.id, a.id, `${a.name}: ${metric === 'revenue' ? '売上' : '費用'} ${yen(value(a))}、目標との差 ${diff === null ? 'なし' : yen(diff)}`, (
                                <TipBody
                                  title={a.name}
                                  sub={subOf(a, unitOf(a.unit_id)?.name ?? '')}
                                  rows={[
                                    [metric === 'revenue' ? '売上（満額）' : '費用（満額）', yen(value(a))],
                                    ['利益（加重見込）', yen(current)],
                                    ['目標（加重見込）', target === null ? 'なし' : yen(target)],
                                    ['目標との差', diff === null ? '—' : signedYen(diff)],
                                  ]}
                                />
                              ))}
                            >
                              {/* 面の色の隙間で、隣の施策と分ける */}
                              <rect
                                x={rect.x + 1.5}
                                y={rect.y + 1.5}
                                width={Math.max(0, rect.w - 3)}
                                height={Math.max(0, rect.h - 3)}
                                rx={6}
                                fill={fill}
                                stroke={active(a.id) ? ink.primary : 'none'}
                                strokeWidth={2}
                              />
                              {rect.w > 56 && rect.h > 24 && (
                                <text x={rect.x + 8} y={rect.y + 18} fontSize={12} fontWeight={500} fill={dark ? '#ffffff' : ink.primary} pointerEvents="none">
                                  {clip(a.name, rect.w - 16, 12)}
                                </text>
                              )}
                              {rect.w > 56 && rect.h > 42 && (
                                <text x={rect.x + 8} y={rect.y + 34} fontSize={11} fill={dark ? '#ffffff' : ink.secondary} pointerEvents="none" className="tabular-nums">
                                  {/* 差まで入らない幅なら、金額だけにする */}
                                  {diff !== null && textWidth(`${man(value(a))} ／ 差 ${man(diff, true)}`, 11) <= rect.w - 16
                                    ? `${man(value(a))} ／ 差 ${man(diff, true)}`
                                    : clip(man(value(a)), rect.w - 16, 11)}
                                </text>
                              )}
                            </g>
                          )
                        })}
                      </g>
                    )
                  })}
                </g>
              )
            })}
          </svg>
          <Tooltip tip={tip} />
        </div>
      )}
    </ChartCard>
  )
}

// --- 振れ幅（楽観 〜 悲観） ---

const LABEL_W = 230
const ROW_H = 34

/** 施策ごとに、悲観〜楽観の利益を帯、加重見込を点で描く。振れ幅の大きい順 */
function RangeChart({ items, unitName }: { items: RiskActivity[]; unitName: (id: number) => string }) {
  const { tip, active, props } = useMarks()
  const { rows, rest } = rangeRows(items)
  if (rows.length === 0) return <Empty>金額のある施策がありません</Empty>
  const xs = niceTicks(Math.min(0, ...rows.map((r) => r.pessimistic)), Math.max(0, ...rows.map((r) => r.optimistic)))
  const [x0, x1] = [xs[0], xs[xs.length - 1]]
  const top = 28
  const height = top + rows.length * ROW_H + 8
  const px = (v: number) => LABEL_W + ((v - x0) / (x1 - x0)) * (W - LABEL_W - 24)

  return (
    <ChartCard
      legend={
        <>
          <span className="inline-flex items-center gap-1.5">
            <span className="inline-block h-2 w-6 rounded-full" style={{ background: ink.accentSoft }} />
            悲観 〜 楽観（利益）
          </span>
          <Swatch color={ink.accent} label="加重見込" />
          <span className="text-slate-500">⚠ 段階に対して状況が悪い</span>
        </>
      }
      footer={`振れ幅（楽観 − 悲観）の大きい順に ${rows.length} 件を表示しています${rest > 0 ? `（ほか ${rest} 件）` : ''}。帯が長い施策ほど、見込が確かでない部分が多いということです。`}
    >
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label="振れ幅（施策ごとの悲観・加重見込・楽観の利益）">
          {xs.map((v) => (
            <g key={v}>
              <line x1={px(v)} x2={px(v)} y1={top - 6} y2={height - 6} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={px(v)} y={top - 12} textAnchor="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                {man(v)}
              </text>
            </g>
          ))}
          {rows.map((r, i) => {
            const a = r.activity
            const y = top + i * ROW_H + ROW_H / 2
            const on = active(a.id)
            return (
              <g
                key={a.id}
                {...props(a.id, a.id, `${a.name}: 悲観 ${yen(r.pessimistic)}、加重見込 ${yen(r.weighted)}、楽観 ${yen(r.optimistic)}`, (
                  <TipBody
                    title={a.name}
                    sub={subOf(a, unitName(a.unit_id))}
                    rows={[
                      ['楽観', yen(r.optimistic)],
                      ['加重見込', yen(r.weighted)],
                      ['悲観', yen(r.pessimistic)],
                      ['振れ幅', yen(r.spread)],
                      ['警告', a.warning_count > 0 ? `${a.warning_count} 件${a.bad_for_level ? '（段階に対して状況が悪い）' : ''}` : 'なし'],
                    ]}
                  />
                ))}
              >
                <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={on ? '#f6f5ff' : 'transparent'} />
                <text x={8} y={y} dominantBaseline="middle" fontSize={12} fill={ink.primary}>
                  {a.bad_for_level ? '⚠ ' : ''}
                  {clip(a.name, LABEL_W - 24 - (a.bad_for_level ? 16 : 0), 12)}
                </text>
                <rect x={px(r.pessimistic)} y={y - 5} width={Math.max(4, px(r.optimistic) - px(r.pessimistic))} height={10} rx={5} fill={ink.accentSoft} />
                <circle cx={px(r.weighted)} cy={y} r={6} fill={ink.accent} stroke={ink.surface} strokeWidth={2} />
              </g>
            )
          })}
        </svg>
        <Tooltip tip={tip} />
      </div>
    </ChartCard>
  )
}

// --- 増減の内訳（ウォーターフォール） ---

/** 比較（目標・前回の見込）の利益から、施策ごとの増減を積み上げて、今回の見込の利益に着地する（加重見込） */
function WaterfallChart({
  items,
  startLabel,
  compareTo,
  onCompareTo,
  hasPrevious,
}: {
  items: RiskActivity[]
  startLabel: string
  compareTo: 'target' | 'previous'
  onCompareTo: (v: 'target' | 'previous') => void
  hasPrevious: boolean
}) {
  const { tip, active, props } = useMarks()
  const steps = waterfallSteps(items, startLabel)
  const values = steps.flatMap((s) => [s.from, s.to])
  const xs = niceTicks(Math.min(0, ...values), Math.max(0, ...values))
  const [x0, x1] = [xs[0], xs[xs.length - 1]]
  const top = 28
  const height = top + steps.length * ROW_H + 8
  const px = (v: number) => LABEL_W + ((v - x0) / (x1 - x0)) * (W - LABEL_W - 90)

  return (
    <ChartCard
      legend={
        <>
          <Swatch shape="square" color={totalColor} label="合計" />
          <Swatch shape="square" color={upColor} label="増加" />
          <Swatch shape="square" color={downColor} label="減少" />
        </>
      }
      actions={
        hasPrevious ? (
          <Segmented
            label="比較"
            value={compareTo}
            options={[
              ['target', '目標から'],
              ['previous', '前回の見込から'],
            ]}
            onChange={onCompareTo}
          />
        ) : undefined
      }
      footer="年間の利益（加重見込）の増減です。増減の大きい施策を並べ、残りは「その他」にまとめています。比較のない施策は、比較を 0 として数えます。"
    >
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label={`増減の内訳（${startLabel}から今回の見込へ）`}>
          {xs.map((v) => (
            <g key={v}>
              <line x1={px(v)} x2={px(v)} y1={top - 6} y2={height - 6} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={px(v)} y={top - 12} textAnchor="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                {man(v)}
              </text>
            </g>
          ))}
          {steps.map((s, i) => {
            const y = top + i * ROW_H + ROW_H / 2
            const left = px(Math.min(s.from, s.to))
            const width = Math.max(3, Math.abs(px(s.to) - px(s.from)))
            const delta = s.to - s.from
            const color = s.kind === 'total' ? totalColor : delta >= 0 ? upColor : downColor
            const next = steps[i + 1]
            return (
              <g
                key={s.key}
                {...props(s.key, s.activityId, `${s.label}: ${s.kind === 'total' ? yen(s.to) : signedYen(delta)}`, (
                  <TipBody
                    title={s.label}
                    rows={s.kind === 'total' ? [['利益（加重見込）', yen(s.to)]] : [['増減', signedYen(delta)], ['累計', yen(s.to)]]}
                    link={s.activityId !== null}
                  />
                ))}
              >
                <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={active(s.key) ? '#f6f5ff' : 'transparent'} />
                <text x={8} y={y} dominantBaseline="middle" fontSize={12} fontWeight={s.kind === 'total' ? 600 : 400} fill={ink.primary}>
                  {clip(s.label, LABEL_W - 24, 12)}
                </text>
                <rect x={left} y={y - 9} width={width} height={18} rx={4} fill={color} />
                <text x={left + width + 6} y={y} dominantBaseline="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                  {s.kind === 'total' ? man(s.to) : man(delta, true)}
                </text>
                {/* 次の棒への細い線（累計のつながり） */}
                {next && next.kind === 'delta' && <line x1={px(s.to)} x2={px(s.to)} y1={y + 9} y2={y + ROW_H - 9} stroke={ink.axis} strokeWidth={1} />}
              </g>
            )
          })}
        </svg>
        <Tooltip tip={tip} />
      </div>
    </ChartCard>
  )
}

// --- スケジュール（ガント） ---

type MilestoneItem = { id: number; activity_id: number; name: string; due_date: string; overdue: boolean; delayed: boolean }

/** 施策の期間を帯、完了していないマイルストーンを菱形で描く。年度（4月〜翌3月）の範囲 */
function TimelineChart({ activities, scenario, unitName }: { activities: Activity[]; scenario: Scenario; unitName: (id: number) => string }) {
  const { tip, active, props } = useMarks()
  const dated = activities
    .filter((a) => a.start_date || a.end_date)
    .sort((p, q) => (p.start_date ?? p.end_date ?? '').localeCompare(q.start_date ?? q.end_date ?? '') || p.code.localeCompare(q.code))
    .slice(0, 40)
  const ids = dated.map((a) => a.id).slice(0, 200)
  const milestones = useApi<{ items: MilestoneItem[] }>(scenario.is_active && ids.length > 0 ? `/scenarios/${scenario.id}/milestones?activity_ids=${ids.join(',')}` : null)
  if (dated.length === 0) return <Empty>開始日・終了日のある施策がありません（プロジェクト型の施策に期間を入れると表示されます）</Empty>
  const fy = scenario.fiscal_year
  const months = fiscalMonths(fy)
  const top = 30
  const height = top + dated.length * ROW_H + 8
  const plotW = W - LABEL_W - 16
  const px = (p: number) => LABEL_W + Math.min(1, Math.max(0, p)) * plotW
  const today = yearPosition(todayInTokyo(), fy)
  const byActivity = new Map<number, MilestoneItem[]>()
  for (const m of milestones.data?.items ?? []) byActivity.set(m.activity_id, [...(byActivity.get(m.activity_id) ?? []), m])
  const types = (Object.keys(typeColor) as ActivityType[]).filter((t) => dated.some((a) => a.activity_type === t))

  return (
    <ChartCard
      legend={
        <>
          {types.map((t) => (
            <Swatch key={t} color={typeColor[t]} shape="line" label={activityTypeLabels[t]} />
          ))}
          <span className="inline-flex items-center gap-1.5">
            <svg width="12" height="12" aria-hidden="true">
              <rect x="2" y="2" width="8" height="8" transform="rotate(45 6 6)" fill={ink.surface} stroke={ink.primary} strokeWidth="1.5" />
            </svg>
            マイルストーン
          </span>
          <span className="inline-flex items-center gap-1.5">
            <svg width="12" height="12" aria-hidden="true">
              <rect x="2" y="2" width="8" height="8" transform="rotate(45 6 6)" fill={downsideColor} />
            </svg>
            期日超過・遅延
          </span>
        </>
      }
      footer={`${fy}年度（4月〜翌3月）の範囲です。開始日の順に、期間のある施策を ${dated.length} 件まで表示しています。マイルストーンは完了していないもので、今回の見込のときだけ表示します。`}
    >
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label={`スケジュール（${fy}年度）`}>
          {months.map((m, i) => (
            <g key={m}>
              <line x1={px(i / 12)} x2={px(i / 12)} y1={top - 8} y2={height - 6} stroke={ink.grid} strokeWidth={1} />
              <text x={px((i + 0.5) / 12)} y={top - 14} textAnchor="middle" fontSize={11} fill={ink.secondary}>
                {monthLabel(m)}
              </text>
            </g>
          ))}
          {today >= 0 && today <= 1 && (
            <g>
              <line x1={px(today)} x2={px(today)} y1={top - 8} y2={height - 6} stroke={ink.accent} strokeWidth={1.5} />
              <rect x={px(today) - 16} y={height - 20} width={32} height={16} rx={8} fill={ink.accent} />
              <text x={px(today)} y={height - 12} textAnchor="middle" dominantBaseline="middle" fontSize={10} fill="#ffffff">
                今日
              </text>
            </g>
          )}
          {dated.map((a, i) => {
            const y = top + i * ROW_H + ROW_H / 2
            const start = a.start_date ? yearPosition(a.start_date, fy) : 0
            const end = a.end_date ? yearPosition(a.end_date, fy) + 1 / 365 : 1
            const visible = end > 0 && start < 1
            const ms = byActivity.get(a.id) ?? []
            return (
              <g key={a.id}>
                <g
                  {...props(a.id, a.id, `${a.name}: ${a.start_date ?? '開始日なし'} 〜 ${a.end_date ?? '終了日なし'}`, (
                    <TipBody
                      title={a.name}
                      sub={subOf(a, unitName(a.unit_id))}
                      rows={[
                        ['期間', `${a.start_date ?? '—'} 〜 ${a.end_date ?? '—'}`],
                        ['マイルストーン（未完了）', `${ms.length} 件`],
                      ]}
                    />
                  ))}
                >
                  <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={active(a.id) ? '#f6f5ff' : 'transparent'} />
                  <text x={8} y={y} dominantBaseline="middle" fontSize={12} fill={ink.primary}>
                    {clip(a.name, LABEL_W - 24, 12)}
                  </text>
                  {visible && <rect x={px(start)} y={y - 7} width={Math.max(4, px(end) - px(start))} height={14} rx={7} fill={typeColor[a.activity_type]} fillOpacity={0.85} />}
                </g>
                {ms
                  .filter((m) => yearPosition(m.due_date, fy) >= 0 && yearPosition(m.due_date, fy) <= 1)
                  .map((m) => {
                    const x = px(yearPosition(m.due_date, fy))
                    const late = m.overdue || m.delayed
                    return (
                      <rect
                        key={m.id}
                        x={x - 5}
                        y={y - 5}
                        width={10}
                        height={10}
                        transform={`rotate(45 ${x} ${y})`}
                        fill={late ? downsideColor : ink.surface}
                        stroke={late ? ink.surface : ink.primary}
                        strokeWidth={1.5}
                        {...props(`m${m.id}`, a.id, `${m.name}: 期日 ${m.due_date}${late ? '（期日超過・遅延）' : ''}`, (
                          <TipBody title={m.name} sub={a.name} rows={[['期日', m.due_date], ['状態', m.delayed ? '遅延' : m.overdue ? '期日超過' : '予定どおり']]} />
                        ))}
                      />
                    )
                  })}
              </g>
            )
          })}
        </svg>
        <Tooltip tip={tip} />
      </div>
    </ChartCard>
  )
}

// --- 確度の推移（月ごとの売上を段階で積み上げ） ---

/** 月ごとの売上（満額）を、実績・確度の段階・ダウンサイドで積み上げる。ダウンサイドは 0 の下に出す */
function PipelineChart({ items, months, levels, actualThrough }: { items: RiskActivity[]; months: string[]; levels: RiskReport['levels']; actualThrough: string | null }) {
  const { tip, active, props } = useMarks()
  const data = pipelineByMonth(items, months)
  const keys = ['actual', ...levels.map((l) => l.code)]
  const colorOf = (k: string) => (k === 'actual' ? actualColor : k === 'downside' ? downsideColor : levelRamp[Math.min(levels.findIndex((l) => l.code === k), levelRamp.length - 1)])
  const nameOf = (k: string) => (k === 'actual' ? '実績' : k === 'downside' ? 'ダウンサイド' : `${k} ${levels.find((l) => l.code === k)?.name ?? ''}`)
  const totals = months.map((m) => keys.reduce((s, k) => s + Math.max(0, data.get(m)?.get(k) ?? 0), 0))
  const downs = months.map((m) => Math.min(0, data.get(m)?.get('downside') ?? 0))
  if (totals.every((t) => t === 0) && downs.every((d) => d === 0)) return <Empty>売上のある施策がありません</Empty>
  // 下（ダウンサイド）は、目盛りの区切りまで広げずにデータの分だけ取る（小さいダウンサイドで図が縮まないように）
  const minDown = Math.min(0, ...downs)
  const ys = niceTicks(minDown, Math.max(...totals)).filter((v) => v >= 0 || v >= minDown)
  const [y0, y1] = [Math.min(minDown * 1.15, ys[0]), ys[ys.length - 1]]
  const margin = { top: 16, bottom: 36, left: 84, right: 16 }
  const height = 420
  const slot = (W - margin.left - margin.right) / months.length
  const barW = 28
  const py = (v: number) => margin.top + ((y1 - v) / (y1 - y0)) * (height - margin.top - margin.bottom)
  const usedKeys = [...keys.filter((k) => months.some((m) => (data.get(m)?.get(k) ?? 0) > 0)), ...(downs.some((d) => d < 0) ? ['downside'] : [])]

  return (
    <ChartCard
      legend={usedKeys.map((k) => (
        <Swatch key={k} shape="square" color={colorOf(k)} label={nameOf(k)} />
      ))}
      footer="月ごとの売上（満額）を、実績と確度の段階に分けて積み上げています。濃い紫ほど確かです。下期が淡い色ばかりなら、見込が不確かな部分に頼っているということです。"
    >
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label="確度の推移（月ごとの売上を段階で積み上げ）">
          {ys.map((v) => (
            <g key={v}>
              <line x1={margin.left} x2={W - margin.right} y1={py(v)} y2={py(v)} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={margin.left - 10} y={py(v)} textAnchor="end" dominantBaseline="middle" fontSize={12} fill={ink.secondary} className="tabular-nums">
                {man(v)}
              </text>
            </g>
          ))}
          {months.map((m, i) => {
            const cx = margin.left + slot * (i + 0.5)
            const parts = data.get(m) ?? new Map<string, number>()
            let base = 0
            const segs = keys
              .map((k) => ({ k, v: Math.max(0, parts.get(k) ?? 0) }))
              .filter((s) => s.v > 0)
              .map((s) => {
                const seg = { ...s, from: base, to: base + s.v }
                base += s.v
                return seg
              })
            const down = Math.min(0, parts.get('downside') ?? 0)
            const isActual = actualThrough !== null && m <= actualThrough
            return (
              <g
                key={m}
                {...props(m, null, `${monthLabel(m)}: 売上 ${yen(totals[i])}`, (
                  <TipBody
                    title={`${monthLabel(m)}${isActual ? '（実績）' : ''}`}
                    rows={[...segs.map((s): [string, string] => [nameOf(s.k), yen(s.v)]), ...(down < 0 ? [['ダウンサイド', yen(down)] as [string, string]] : []), ['合計', yen(totals[i] + down)]]}
                    link={false}
                  />
                ))}
              >
                <rect x={cx - slot / 2 + 2} y={margin.top} width={slot - 4} height={height - margin.top - margin.bottom} rx={8} fill={active(m) ? '#f6f5ff' : 'transparent'} />
                {segs.map((s, j) => {
                  const yTop = py(s.to)
                  const h = Math.max(0, py(s.from) - py(s.to) - (j < segs.length - 1 ? 2 : 0))
                  return <rect key={s.k} x={cx - barW / 2} y={yTop + (j < segs.length - 1 ? 0 : 0)} width={barW} height={h} rx={j === segs.length - 1 ? 4 : 1.5} fill={colorOf(s.k)} />
                })}
                {down < 0 && <rect x={cx - barW / 2} y={py(0) + 2} width={barW} height={Math.max(0, py(down) - py(0) - 2)} rx={3} fill={downsideColor} />}
                <text x={cx} y={height - margin.bottom + 18} textAnchor="middle" fontSize={11} fill={isActual ? ink.muted : ink.secondary}>
                  {monthLabel(m)}
                </text>
              </g>
            )
          })}
        </svg>
        <Tooltip tip={tip} />
      </div>
    </ChartCard>
  )
}
