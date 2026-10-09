import { useState, type FocusEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { query } from '../../api/client'
import { activityTypeLabels, type ActivityType, type List, type RiskActivity, type RiskReport, type Scenario, type TreeNode, type Unit } from '../../api/types'
import { RestrictedNote } from '../../components/RestrictedNote'
import { Card, Empty, ErrorMessage, Loading, cx } from '../../components/ui'
import { bubblePoints, diffBucket, niceTicks, profit, squarify, type DiffBucket, type Rect } from '../../lib/activityCharts'
import { navigate } from '../../lib/router'
import { currentFiscalYear, defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

export type ChartView = 'portfolio' | 'treemap'

// 色（dataviz の既定のパレット。docs/plan.md「2.22」）
// 施策のタイプ: 分類の色の 1〜3 番（すべての組で見分けられることを確かめた順番）。aqua は面とのコントラストが 3:1 未満のため、凡例・ツールチップ・表で補う
const typeColors: Record<ActivityType, string> = { recurring: '#2a78d6', project: '#eb6834', cost_pool: '#1baf7a' }
// 目標との差: 青（上振れ）↔ 灰（目標どおり）↔ 赤（下振れ）の発散の色
const diffColors: Record<DiffBucket, string> = { [-2]: '#c4302f', [-1]: '#f0a3a2', 0: '#e4e3de', 1: '#9ec5f4', 2: '#256abf' }
const noTargetColor = '#f0efec'
const diffLabels: Record<DiffBucket, string> = { [-2]: '−20% 以下', [-1]: '−20〜−5%', 0: '±5% 未満', 1: '+5〜+20%', 2: '+20% 以上' }
const ink = { primary: '#0b0b0b', secondary: '#52514e', muted: '#898781', grid: '#e1e0d9', axis: '#c3c2b7', surface: '#ffffff' }

/** 金額を「1,234万」の形にする（図の目盛り・ラベル用） */
function man(n: number): string {
  const v = Math.round(n / 10_000)
  return `${v.toLocaleString('ja-JP')}万`
}

/**
 * 施策の一覧の「図で見る」（docs/plan.md「2.22」）。今回の見込（なければ今年度の最新見込）の年間の金額を、
 * 一覧の絞り込み（activityIds）に合わせて図にする。金額はリスク画面と同じ集計（GET /api/reports/risk）を使う。
 */
export function ActivityCharts({ view, activityIds }: { view: ChartView; activityIds: Set<number> }) {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const units = useApi<List<Unit>>('/units')
  const segments = useApi<List<TreeNode>>('/segments')
  const list = scenarios.data?.items ?? []
  const scenario = list.find((s) => s.is_active) ?? defaultScenarios(list.filter((s) => s.fiscal_year === currentFiscalYear())).latest ?? list[0]
  const target = scenario ? defaultScenarios(list.filter((s) => s.fiscal_year === scenario.fiscal_year)).base : undefined
  const compareId = target && target.id !== scenario?.id ? target.id : undefined
  const report = useApi<RiskReport>(scenario ? `/reports/risk${query({ scenario_id: scenario.id, compare_id: compareId })}` : null)

  const error = scenarios.error ?? units.error ?? segments.error ?? report.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !units.data || !segments.data) return <Loading />
  if (!scenario) return <Empty>シナリオがまだありません。</Empty>
  if (!report.data) return <Loading />

  const items = report.data.activities.filter((a) => activityIds.has(a.id))
  const unitById = new Map(units.data.items.map((u) => [u.id, u]))
  const segmentName = new Map(segments.data.items.map((s) => [s.id, s.name]))
  return (
    <div className="space-y-3">
      <p className="text-xs text-slate-500">
        {scenarioLabel(scenario)}（{scenario.fiscal_year}年度・年間、実績の月は実績）
        {view === 'treemap' && <> ／ 目標: {compareId && target ? scenarioLabel(target) : 'なし'}</>}
      </p>
      <RestrictedNote hidden={report.data.restricted_hidden} />
      {items.length === 0 ? (
        <Empty>条件に合う施策がありません</Empty>
      ) : view === 'portfolio' ? (
        <PortfolioMap items={items} unitName={(id) => unitById.get(id)?.name ?? ''} levels={report.data.levels} />
      ) : (
        <Treemap items={items} unitOf={(id) => unitById.get(id)} segmentName={(id) => segmentName.get(id) ?? ''} hasTarget={compareId !== undefined} />
      )}
    </div>
  )
}

// --- ツールチップ ---

type Tip = { x: number; y: number; content: ReactNode; flip: boolean }

/** 図の上に重ねるツールチップ。位置は図の枠からの px */
function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  return (
    <div
      role="tooltip"
      className="pointer-events-none absolute z-20 w-64 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs text-slate-700 shadow-lg"
      // 右端に近いときは、カーソルの左に出して枠からはみ出さないようにする（幅は w-64 = 256px）
      style={{ left: tip.flip ? Math.max(4, tip.x - 14 - 256) : tip.x + 14, top: tip.y + 14 }}
    >
      {tip.content}
    </div>
  )
}

/** マウス・キーボードの両方で、図の印にツールチップを出し、クリック・Enter で施策を開く */
function useMarks() {
  const [tip, setTip] = useState<Tip | null>(null)
  const [active, setActive] = useState<number | null>(null)
  const at = (e: MouseEvent | FocusEvent, id: number, content: ReactNode) => {
    const box = (e.currentTarget as Element).closest('[data-chart]')!.getBoundingClientRect()
    const p = 'clientX' in e ? { x: e.clientX, y: e.clientY } : (() => {
      const r = (e.currentTarget as Element).getBoundingClientRect()
      return { x: r.left + r.width / 2, y: r.top + r.height / 2 }
    })()
    setActive(id)
    setTip({ x: p.x - box.left, y: p.y - box.top, content, flip: p.x - box.left + 14 + 256 > box.width })
  }
  const props = (id: number, label: string, content: ReactNode) => ({
    tabIndex: 0,
    role: 'link',
    'aria-label': label,
    onMouseEnter: (e: MouseEvent) => at(e, id, content),
    onMouseMove: (e: MouseEvent) => at(e, id, content),
    onFocus: (e: FocusEvent) => at(e, id, content),
    onMouseLeave: () => (setTip(null), setActive(null)),
    onBlur: () => (setTip(null), setActive(null)),
    onClick: () => navigate(`/activities/${id}`),
    onKeyDown: (e: KeyboardEvent) => e.key === 'Enter' && navigate(`/activities/${id}`),
    style: { cursor: 'pointer', outline: 'none' },
  })
  return { tip, active, props }
}

function TipBody({ a, unit, rows }: { a: RiskActivity; unit: string; rows: [string, string][] }) {
  return (
    <>
      <div className="font-semibold text-slate-900">{a.name}</div>
      <div className="mb-1 text-slate-500">
        {a.code} ・ {unit} ・ {activityTypeLabels[a.activity_type]}
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-slate-500">{k}</dt>
            <dd className="text-right tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      <div className="mt-1 text-[11px] text-slate-400">クリックで施策を開く</div>
    </>
  )
}

const yen = (n: number) => `${Math.round(n).toLocaleString('ja-JP')} 円`

// --- ポートフォリオ・マップ ---

const W = 960
const H = 480
const M = { top: 40, right: 24, bottom: 44, left: 84 }
// 丸が枠からはみ出さないよう、描く範囲の内側に取る余白（いちばん大きい丸の半径）
const PAD = 32

/** 横軸は確度の割合、縦軸は年間の利益、丸の大きさは売上、色は施策のタイプ */
function PortfolioMap({ items, unitName, levels }: { items: RiskActivity[]; unitName: (id: number) => string; levels: RiskReport['levels'] }) {
  const { tip, active, props } = useMarks()
  const { points, skipped } = bubblePoints(items)
  if (points.length === 0) return <Empty>売上のある施策がありません（ポートフォリオ・マップは売上のある施策を表示します）</Empty>
  const ys = niceTicks(Math.min(0, ...points.map((p) => p.profit)), Math.max(0, ...points.map((p) => p.profit)))
  const [y0, y1] = [ys[0], ys[ys.length - 1]]
  const maxRevenue = Math.max(...points.map((p) => p.revenue))
  const px = (c: number) => M.left + PAD + c * (W - M.left - M.right - PAD * 2)
  const py = (v: number) => M.top + PAD + ((y1 - v) / (y1 - y0)) * (H - M.top - M.bottom - PAD * 2)
  const r = (rev: number) => 5 + 26 * Math.sqrt(rev / maxRevenue)
  // 直接のラベルは、売上の大きい5件だけ（すべてには付けない）
  const labeled = new Set(points.slice(0, 5).map((p) => p.activity.id))
  const types = (Object.keys(typeColors) as ActivityType[]).filter((t) => points.some((p) => p.activity.activity_type === t))
  const quadrant = (x: number, y: number, text: string, anchor: 'start' | 'end') => (
    <text x={x} y={y} textAnchor={anchor} fontSize={12} fill={ink.muted}>
      {text}
    </text>
  )

  return (
    <Card>
      <div className="mb-2 flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-slate-600" aria-label="凡例">
        {types.map((t) => (
          <span key={t} className="inline-flex items-center gap-1.5">
            <span className="inline-block size-3 rounded-full" style={{ background: typeColors[t] }} />
            {activityTypeLabels[t]}
          </span>
        ))}
        <span className="inline-flex items-center gap-1.5 text-slate-500">
          <svg width="26" height="14" aria-hidden="true">
            <circle cx="5" cy="9" r="4" fill="none" stroke={ink.muted} />
            <circle cx="18" cy="7" r="7" fill="none" stroke={ink.muted} />
          </svg>
          丸の大きさ: 売上（満額）
        </span>
      </div>
      <div className="relative" data-chart>
        <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="group" aria-label="ポートフォリオ・マップ（横軸: 確度の割合、縦軸: 年間の利益、丸の大きさ: 売上）">
          {/* 目盛りと格子 */}
          {ys.map((v) => (
            <g key={v}>
              <line x1={M.left} x2={W - M.right} y1={py(v)} y2={py(v)} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={M.left - 8} y={py(v)} textAnchor="end" dominantBaseline="middle" fontSize={12} fill={ink.secondary} className="tabular-nums">
                {man(v)}
              </text>
            </g>
          ))}
          {[0, 0.25, 0.5, 0.75, 1].map((c) => (
            <g key={c}>
              <line x1={px(c)} x2={px(c)} y1={M.top} y2={H - M.bottom} stroke={c === 0.5 ? ink.axis : ink.grid} strokeWidth={1} />
              <text x={px(c)} y={H - M.bottom + 18} textAnchor="middle" fontSize={12} fill={ink.secondary}>
                {c * 100}%
              </text>
            </g>
          ))}
          <text x={(M.left + W - M.right) / 2} y={H - 6} textAnchor="middle" fontSize={12} fill={ink.secondary}>
            確度の割合（加重見込 ÷ 満額、売上） →
          </text>
          <text x={16} y={(M.top + H - M.bottom) / 2} textAnchor="middle" fontSize={12} fill={ink.secondary} transform={`rotate(-90 16 ${(M.top + H - M.bottom) / 2})`}>
            年間の利益（満額） →
          </text>
          {/* 象限の名前 */}
          {quadrant(W - M.right, M.top - 14, '確実で利益が大きい ↗', 'end')}
          {quadrant(M.left, M.top - 14, '↖ 利益は大きいが不確か', 'start')}
          {y0 < 0 && quadrant(W - M.right - 8, H - M.bottom - 10, '確実だが赤字', 'end')}
          {y0 < 0 && quadrant(M.left + 8, H - M.bottom - 10, '不確かで赤字', 'start')}

          {/* 丸（大きい順に描く）。重なりは面の色の輪で分ける */}
          {points.map((p) => {
            const a = p.activity
            const level = levels.find((l) => l.code === a.confidence_level)
            return (
              <circle
                key={a.id}
                cx={px(p.certainty)}
                cy={py(p.profit)}
                r={r(p.revenue)}
                fill={typeColors[a.activity_type]}
                fillOpacity={active === null || active === a.id ? 0.85 : 0.35}
                stroke={active === a.id ? ink.primary : ink.surface}
                strokeWidth={2}
                {...props(a.id, `${a.name}: 確度の割合 ${Math.round(p.certainty * 100)}%、利益 ${yen(p.profit)}、売上 ${yen(p.revenue)}`, (
                  <TipBody
                    a={a}
                    unit={unitName(a.unit_id)}
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
                y={py(p.profit) - r(p.revenue) - 6}
                textAnchor={p.certainty > 0.85 ? 'end' : p.certainty < 0.15 ? 'start' : 'middle'}
                fontSize={12}
                fill={ink.primary}
                pointerEvents="none"
              >
                {p.activity.name.length > 14 ? `${p.activity.name.slice(0, 14)}…` : p.activity.name}
              </text>
            ))}
        </svg>
        <Tooltip tip={tip} />
      </div>
      {skipped > 0 && <p className="mt-2 text-xs text-slate-500">売上のない施策（費用だけの施策など）{skipped} 件は表示していません。ツリーマップの「費用」で見られます。</p>}
    </Card>
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

  // 入れ子にまとめる
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
  const inset = (r: Rect, top: number, pad = 2): Rect => ({ x: r.x + pad, y: r.y + top, w: Math.max(0, r.w - pad * 2), h: Math.max(0, r.h - top - pad) })

  return (
    <Card>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-600" aria-label="凡例">
          <span className="text-slate-500">目標との差（加重見込の利益）:</span>
          {([-2, -1, 0, 1, 2] as DiffBucket[]).map((b) => (
            <span key={b} className="inline-flex items-center gap-1">
              <span className="inline-block size-3 rounded-sm" style={{ background: diffColors[b] }} />
              {diffLabels[b]}
            </span>
          ))}
          <span className="inline-flex items-center gap-1">
            <span className="inline-block size-3 rounded-sm border border-slate-300" style={{ background: noTargetColor }} />
            目標なし
          </span>
        </div>
        <div role="group" aria-label="面積" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
          {(['revenue', 'expense'] as Metric[]).map((m) => (
            <button
              key={m}
              type="button"
              aria-pressed={metric === m}
              onClick={() => setMetric(m)}
              className={cx('rounded px-2.5 py-1 text-xs font-medium', metric === m ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
            >
              面積: {m === 'revenue' ? '売上' : '費用'}
            </button>
          ))}
        </div>
      </div>
      {segs.length === 0 ? (
        <Empty>{metric === 'revenue' ? '売上' : '費用'}のある施策がありません</Empty>
      ) : (
        <div className="relative" data-chart>
          <svg viewBox={`0 0 ${TW} ${TH}`} className="h-auto w-full" role="group" aria-label={`ツリーマップ（面積: ${metric === 'revenue' ? '売上' : '費用'}、色: 目標との差）`}>
            {segs.map((s) => {
              const segBox = inset(s.rect, 18)
              const units = squarify(
                [...s.units].map(([id, list]) => ({ id, list, value: sum(list) })),
                segBox,
              )
              return (
                <g key={s.id}>
                  <rect x={s.rect.x + 1} y={s.rect.y + 1} width={Math.max(0, s.rect.w - 2)} height={Math.max(0, s.rect.h - 2)} fill="#f9f9f7" stroke={ink.grid} />
                  {s.rect.w > 60 && (
                    <text x={s.rect.x + 6} y={s.rect.y + 13} fontSize={11} fontWeight={600} fill={ink.secondary}>
                      {clip(segmentName(s.id) || '（セグメントなし）', s.rect.w - 12, 11)}
                    </text>
                  )}
                  {units.map((u) => {
                    const unitBox = inset(u.rect, 15, 1)
                    const acts = squarify(
                      u.list.map((a) => ({ id: a.id, a, value: value(a) })),
                      unitBox,
                    )
                    return (
                      <g key={u.id}>
                        {u.rect.w > 50 && (
                          <text x={u.rect.x + 4} y={u.rect.y + 11} fontSize={10} fill={ink.muted}>
                            {clip(unitOf(u.id)?.name ?? '', u.rect.w - 8, 10)}
                          </text>
                        )}
                        {acts.map(({ a, rect }) => {
                          const current = profit(a.weighted)
                          const target = hasTarget && a.compare ? profit(a.compare) : null
                          const bucket = diffBucket(current, target)
                          const fill = bucket === null ? noTargetColor : diffColors[bucket]
                          const dark = bucket === 2 || bucket === -2
                          const diff = target === null ? null : current - target
                          return (
                            <g
                              key={a.id}
                              {...props(a.id, `${a.name}: ${metric === 'revenue' ? '売上' : '費用'} ${yen(value(a))}、目標との差 ${diff === null ? 'なし' : yen(diff)}`, (
                                <TipBody
                                  a={a}
                                  unit={unitOf(a.unit_id)?.name ?? ''}
                                  rows={[
                                    [metric === 'revenue' ? '売上（満額）' : '費用（満額）', yen(value(a))],
                                    ['利益（加重見込）', yen(current)],
                                    ['目標（加重見込）', target === null ? 'なし' : yen(target)],
                                    ['目標との差', diff === null ? '—' : `${diff > 0 ? '+' : ''}${yen(diff)}`],
                                  ]}
                                />
                              ))}
                            >
                              {/* 面の色の 2px の隙間で、隣の施策と分ける */}
                              <rect
                                x={rect.x + 1}
                                y={rect.y + 1}
                                width={Math.max(0, rect.w - 2)}
                                height={Math.max(0, rect.h - 2)}
                                rx={3}
                                fill={fill}
                                stroke={active === a.id ? ink.primary : 'none'}
                                strokeWidth={2}
                              />
                              {rect.w > 56 && rect.h > 22 && (
                                <text x={rect.x + 6} y={rect.y + 16} fontSize={12} fill={dark ? '#ffffff' : ink.primary} pointerEvents="none">
                                  {clip(a.name, rect.w - 12, 12)}
                                </text>
                              )}
                              {rect.w > 56 && rect.h > 40 && (
                                <text x={rect.x + 6} y={rect.y + 32} fontSize={11} fill={dark ? '#ffffff' : ink.secondary} pointerEvents="none" className="tabular-nums">
                                  {man(value(a))}
                                  {diff !== null && ` ／ 差 ${diff > 0 ? '+' : ''}${man(diff)}`}
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
      <p className="mt-2 text-xs text-slate-500">色は、年間の利益（加重見込）の目標との差の率です。面積が小さく名前を出せない施策は、カーソルを当てると詳細が出ます。</p>
    </Card>
  )
}

/** 幅に収まらない文字は省略する（全角を 1 文字 = fontSize と見なした概算） */
function clip(text: string, width: number, fontSize: number): string {
  const max = Math.floor(width / fontSize)
  if (max <= 1) return ''
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}
