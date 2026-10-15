// ダッシュボードの図（docs/plan.md「2.30」）。依存ライブラリは使わず SVG で描く。
// 文字は色を付けず、色は印（棒・線・面）だけに使う（web/src/lib/chartTheme.ts）。

import { useState, type ReactNode } from 'react'
import { ChartCard, Segmented, Swatch, TipBody, Tooltip, clip, useChartFormat, useMarks, useWidth } from '../../components/chart'
import { Table, cx } from '../../components/ui'
import { niceTicks } from '../../lib/activityCharts'
import { actualColor, downColor, downsideColor, ink, levelRamp, totalColor, trend, upColor } from '../../lib/chartTheme'
import { measureLabels, type Measure } from '../../lib/aggregate'
import { useAmountUnit } from '../../lib/amountUnit'
import { measureValue, type PL, type Step, type VarianceBar } from '../../lib/dashboard'
import { monthLabel } from '../../lib/format'

const toNum = (v: bigint) => Number(v)

/** 図の「表で見る」の切り替え */
export function useTableView() {
  const [table, setTable] = useState(false)
  const toggle = (
    <label className="flex items-center gap-1.5 text-xs text-slate-600">
      <input type="checkbox" className="size-3.5 rounded border-slate-300" checked={table} onChange={(e) => setTable(e.target.checked)} />
      表で見る
    </label>
  )
  return { table, toggle }
}

// --- ② 月次の推移 ---

export type TrendSeries = { current: Map<string, PL>; compare: Map<string, PL> | null; previous: Map<string, PL> | null }

/** 月ごとの棒（実績の月は濃く、見込の月は淡く）と、比較の線・前回の見込の点線 */
export function TrendChart({
  months,
  actualThrough,
  series,
  measure,
  onMeasure,
  labels,
}: {
  months: string[]
  actualThrough: string | null
  series: TrendSeries
  measure: Measure
  onMeasure: (m: Measure) => void
  /** 系列の名前（今回・比較・前回の見込のシナリオの名前かエイリアス） */
  labels: { current: string; compare: string; previous: string }
}) {
  const compareLabel = labels.compare
  const { man, yen } = useChartFormat()
  const { tip, active, props } = useMarks()
  const { table, toggle } = useTableView()
  const [ref, W] = useWidth<HTMLDivElement>()
  const val = (m: Map<string, PL> | null, month: string) => (m ? toNum(measureValue(m.get(month), measure)) : null)
  const values = months.flatMap((m) => [val(series.current, m), val(series.compare, m), val(series.previous, m)].filter((v): v is number => v !== null))
  const ys = niceTicks(Math.min(0, ...values), Math.max(0, ...values), 4)
  const [y0, y1] = [ys[0], ys[ys.length - 1]]
  const H = 240
  const M = { top: 12, right: 12, bottom: 28, left: 52 }
  const step = (W - M.left - M.right) / months.length
  // 幅が狭いときは、月の名前を1つおきに出す
  const every = step < 26 ? 2 : 1
  const py = (v: number) => M.top + (1 - (v - y0) / (y1 - y0 || 1)) * (H - M.top - M.bottom)
  const cx0 = (i: number) => M.left + step * i + step / 2
  const isActual = (m: string) => actualThrough !== null && m <= actualThrough
  const line = (m: Map<string, PL> | null) =>
    m
      ? months
          .map((month, i) => `${i === 0 ? 'M' : 'L'}${cx0(i)},${py(val(m, month) ?? 0)}`)
          .join(' ')
      : ''

  return (
    <ChartCard
      legend={
        <>
          <Swatch shape="square" color={trend.actual} label={`${labels.current}（実績の月）`} />
          <Swatch shape="square" color={trend.plan} label={`${labels.current}（見込の月）`} />
          {series.compare && <Swatch shape="line" color={trend.target} label={compareLabel} />}
          {series.previous && <Swatch shape="line" color={trend.previous} label={`${labels.previous}（点線）`} />}
        </>
      }
      actions={
        <div className="flex items-center gap-3">
          <Segmented label="金額" value={measure} options={(['profit', 'revenue', 'expense'] as Measure[]).map((m) => [m, measureLabels[m]])} onChange={onMeasure} />
          {toggle}
        </div>
      }
    >
      {table ? (
        <div className="overflow-x-auto">
          <Table>
            <thead>
              <tr>
                <th>月</th>
                <th className="text-right">{labels.current}</th>
                {series.compare && <th className="text-right">{compareLabel}</th>}
                {series.previous && <th className="text-right">{labels.previous}</th>}
              </tr>
            </thead>
            <tbody>
              {months.map((m) => (
                <tr key={m}>
                  <td>
                    {monthLabel(m)}
                    {isActual(m) && <span className="ml-1 text-xs text-slate-500">（実績）</span>}
                  </td>
                  <td className="text-right tabular-nums">{yen(val(series.current, m) ?? 0)}</td>
                  {series.compare && <td className="text-right tabular-nums">{yen(val(series.compare, m) ?? 0)}</td>}
                  {series.previous && <td className="text-right tabular-nums">{yen(val(series.previous, m) ?? 0)}</td>}
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="group" aria-label={`月次の推移（${measureLabels[measure]}）`}>
            {ys.map((v) => (
              <g key={v}>
                <line x1={M.left} x2={W - M.right} y1={py(v)} y2={py(v)} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
                <text x={M.left - 8} y={py(v)} textAnchor="end" dominantBaseline="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                  {man(v)}
                </text>
              </g>
            ))}
            {months.map((m, i) => {
              const v = val(series.current, m) ?? 0
              const top = py(Math.max(0, v))
              const height = Math.max(1, Math.abs(py(v) - py(0)))
              const bw = Math.min(28, step * 0.56)
              const rows: [string, string][] = [[labels.current, yen(v)]]
              if (series.compare) rows.push([compareLabel, yen(val(series.compare, m) ?? 0)])
              if (series.previous) rows.push([labels.previous, yen(val(series.previous, m) ?? 0)])
              return (
                <g key={m} {...props(m, null, `${monthLabel(m)}: ${rows.map((r) => r.join(' ')).join('、')}`, <TipBody title={`${monthLabel(m)}${isActual(m) ? '（実績）' : ''}`} rows={rows} link={false} />)}>
                  <rect x={M.left + step * i} y={M.top} width={step} height={H - M.top - M.bottom} fill={active(m) ? '#f6f5ff' : 'transparent'} />
                  <rect x={cx0(i) - bw / 2} y={top} width={bw} height={height} rx={4} fill={isActual(m) ? trend.actual : trend.plan} />
                  {i % every === 0 && (
                    <text x={cx0(i)} y={H - 10} textAnchor="middle" fontSize={11} fill={ink.secondary}>
                      {Number(m.slice(5, 7))}月
                    </text>
                  )}
                </g>
              )
            })}
            {series.compare && <path d={line(series.compare)} fill="none" stroke={trend.target} strokeWidth={2} pointerEvents="none" />}
            {series.previous && <path d={line(series.previous)} fill="none" stroke={trend.previous} strokeWidth={2} strokeDasharray="5 4" pointerEvents="none" />}
            {series.compare &&
              months.map((m, i) => <circle key={m} cx={cx0(i)} cy={py(val(series.compare, m) ?? 0)} r={3.5} fill={trend.target} stroke={ink.surface} strokeWidth={2} pointerEvents="none" />)}
          </svg>
          <Tooltip tip={tip} />
        </div>
      )}
    </ChartCard>
  )
}

// --- ③ どこで差が出ているか ---

const ROW_H = 30
/** 横棒の図の、左の名前の幅（枠の幅の 3 割、最大 200px） */
const labelWidth = (w: number) => Math.min(200, Math.round(w * 0.3))

/** 区分ごとの比較との差（利益）を、0 を中心に左右へ伸ばす横棒。押すと掘り下げる */
export function VarianceChart({
  bars,
  compareLabel,
  currentLabel,
  onSelect,
  limit = 12,
}: {
  bars: VarianceBar[]
  compareLabel: string
  currentLabel: string
  onSelect: (b: VarianceBar) => void
  limit?: number
}) {
  const { man, yen, signedYen } = useChartFormat()
  const { tip, active, props } = useMarks()
  const { table, toggle } = useTableView()
  const [ref, W] = useWidth<HTMLDivElement>()
  const LABEL_W = labelWidth(W)
  const shown = bars.slice(0, limit)
  const diffs = shown.map((b) => toNum(b.diff))
  const xs = niceTicks(Math.min(0, ...diffs), Math.max(0, ...diffs), 4)
  const [x0, x1] = [xs[0], xs[xs.length - 1]]
  const top = 24
  const height = top + shown.length * ROW_H + 8
  const px = (v: number) => LABEL_W + ((v - x0) / (x1 - x0 || 1)) * (W - LABEL_W - 56)
  const hint = (b: VarianceBar) => (b.group.kind === 'activity' ? 'クリックで施策を開く' : b.group.kind === 'unallocated' ? undefined : 'クリックで掘り下げる')
  return (
    <ChartCard
      legend={
        <>
          <Swatch shape="square" color={upColor} label={`${compareLabel}より上振れ`} />
          <Swatch shape="square" color={downColor} label={`${compareLabel}より下振れ`} />
        </>
      }
      actions={toggle}
      footer={`年間の利益の、${compareLabel}との差です。差の大きい順に並べています。${bars.length > limit ? `ほか ${bars.length - limit} 件は表で見られます。` : ''}`}
    >
      {table ? (
        <Table>
          <thead>
            <tr>
              <th>区分</th>
              <th className="text-right">{currentLabel}</th>
              <th className="text-right">{compareLabel}</th>
              <th className="text-right">差</th>
            </tr>
          </thead>
          <tbody>
            {bars.map((b) => (
              <tr key={b.group.key}>
                <td>
                  {b.group.kind === 'unallocated' ? (
                    b.group.name
                  ) : (
                    <button type="button" className="text-left text-indigo-700 hover:underline" onClick={() => onSelect(b)}>
                      {b.group.name}
                    </button>
                  )}
                </td>
                <td className="text-right tabular-nums">{yen(toNum(b.current))}</td>
                <td className="text-right tabular-nums">{yen(toNum(b.compare))}</td>
                <td className={cx('text-right tabular-nums', b.diff > 0n ? 'text-emerald-700' : b.diff < 0n ? 'text-red-600' : 'text-slate-500')}>{signedYen(toNum(b.diff))}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label={`どこで差が出ているか（${compareLabel}との差）`}>
            {xs.map((v) => (
              <g key={v}>
                <line x1={px(v)} x2={px(v)} y1={top - 6} y2={height - 6} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
                <text x={px(v)} y={top - 10} textAnchor="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                  {man(v)}
                </text>
              </g>
            ))}
            {shown.map((b, i) => {
              const y = top + i * ROW_H + ROW_H / 2
              const d = toNum(b.diff)
              const left = px(Math.min(0, d))
              const width = Math.max(2, Math.abs(px(d) - px(0)))
              // 施策の棒は施策の画面へのリンク、そのほかは掘り下げのボタン、未割当は押せない
              const target = b.group.kind === 'activity' ? b.group.id : b.group.kind === 'unallocated' ? null : () => onSelect(b)
              return (
                <g
                  key={b.group.key}
                  {...props(b.group.key, target, `${b.group.name}: 差 ${signedYen(d)}`, (
                    <TipBody title={b.group.name} rows={[[currentLabel, yen(toNum(b.current))], [compareLabel, yen(toNum(b.compare))], ['差', signedYen(d)]]} link={false} hint={hint(b)} />
                  ))}
                >
                  <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={active(b.group.key) ? '#f6f5ff' : 'transparent'} />
                  <text x={8} y={y} dominantBaseline="middle" fontSize={12} fill={ink.primary}>
                    {clip(b.group.name, LABEL_W - 20, 12)}
                  </text>
                  <rect x={left} y={y - 8} width={width} height={16} rx={4} fill={d >= 0 ? upColor : downColor} />
                  <text x={d >= 0 ? px(d) + 6 : px(0) + 6} y={y} dominantBaseline="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                    {man(d, true)}
                  </text>
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

// --- ④ 前回から何が変わったか ---

/** 前回の見込の利益から、区分ごとの増減を積み上げて今回の見込の利益に至る滝の図。区分を押すと絞り込む */
export function WaterfallCard({
  steps,
  mode,
  onMode,
  onSelect,
  footer,
}: {
  steps: Step[]
  mode: 'group' | 'cause'
  onMode: (m: 'group' | 'cause') => void
  onSelect: (s: Step) => void
  footer?: ReactNode
}) {
  const { man, yen, signedYen } = useChartFormat()
  const { tip, active, props } = useMarks()
  const { table, toggle } = useTableView()
  const [ref, W] = useWidth<HTMLDivElement>()
  const LABEL_W = labelWidth(W)
  const values = steps.flatMap((s) => [toNum(s.from), toNum(s.to)])
  const xs = niceTicks(Math.min(0, ...values), Math.max(0, ...values), 4)
  const [x0, x1] = [xs[0], xs[xs.length - 1]]
  const top = 24
  const height = top + steps.length * ROW_H + 8
  const px = (v: number) => LABEL_W + ((v - x0) / (x1 - x0 || 1)) * (W - LABEL_W - 56)
  return (
    <ChartCard
      legend={
        <>
          <Swatch shape="square" color={totalColor} label="前回・今回の合計" />
          <Swatch shape="square" color={upColor} label="増加" />
          <Swatch shape="square" color={downColor} label="減少" />
        </>
      }
      actions={
        <div className="flex items-center gap-3">
          <Segmented
            label="増減の分け方"
            value={mode}
            options={[
              ['group', '区分ごと'],
              ['cause', '要因の分類ごと'],
            ]}
            onChange={onMode}
          />
          {toggle}
        </div>
      }
      footer={footer}
    >
      {table ? (
        <Table>
          <thead>
            <tr>
              <th>区分</th>
              <th className="text-right">増減</th>
              <th className="text-right">累計</th>
            </tr>
          </thead>
          <tbody>
            {steps.map((s) => (
              <tr key={s.key} className={s.kind === 'total' ? 'font-semibold' : undefined}>
                <td>
                  {s.filter ? (
                    <button type="button" className="text-left text-indigo-700 hover:underline" onClick={() => onSelect(s)}>
                      {s.label}
                    </button>
                  ) : (
                    s.label
                  )}
                </td>
                <td className="text-right tabular-nums">{s.kind === 'total' ? '' : signedYen(toNum(s.to - s.from))}</td>
                <td className="text-right tabular-nums">{yen(toNum(s.to))}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label="前回から何が変わったか（利益の増減）">
            {xs.map((v) => (
              <g key={v}>
                <line x1={px(v)} x2={px(v)} y1={top - 6} y2={height - 6} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
                <text x={px(v)} y={top - 10} textAnchor="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                  {man(v)}
                </text>
              </g>
            ))}
            {steps.map((s, i) => {
              const y = top + i * ROW_H + ROW_H / 2
              const from = toNum(s.from)
              const to = toNum(s.to)
              const left = px(Math.min(from, to))
              const width = Math.max(3, Math.abs(px(to) - px(from)))
              const delta = to - from
              const color = s.kind === 'total' ? totalColor : delta >= 0 ? upColor : downColor
              const next = steps[i + 1]
              const target = s.filter ? () => onSelect(s) : null
              return (
                <g
                  key={s.key}
                  {...props(s.key, target, `${s.label}: ${s.kind === 'total' ? yen(to) : signedYen(delta)}`, (
                    <TipBody title={s.label} rows={s.kind === 'total' ? [['利益', yen(to)]] : [['増減', signedYen(delta)], ['累計', yen(to)]]} link={false} hint={s.filter ? 'クリックで絞り込む' : undefined} />
                  ))}
                >
                  <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={active(s.key) ? '#f6f5ff' : 'transparent'} />
                  <text x={8} y={y} dominantBaseline="middle" fontSize={12} fontWeight={s.kind === 'total' ? 600 : 400} fill={ink.primary}>
                    {clip(s.label, LABEL_W - 20, 12)}
                  </text>
                  <rect x={left} y={y - 8} width={width} height={16} rx={4} fill={color} />
                  <text x={left + width + 6} y={y} dominantBaseline="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                    {s.kind === 'total' ? man(to) : man(delta, true)}
                  </text>
                  {next && next.kind === 'delta' && <line x1={px(to)} x2={px(to)} y1={y + 8} y2={y + ROW_H - 8} stroke={ink.axis} strokeWidth={1} />}
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

// --- ⑤ どれくらい確かか ---

/** 売上の確度の構成（実績・段階・ダウンサイド）の積み上げの帯。段階を押すと絞り込む（もう一度押すと外す） */
export function CompositionBar({
  composition,
  levels,
  selected,
  onSelect,
}: {
  composition: Map<string, bigint>
  levels: { code: string; name: string }[]
  /** 選んだ段階（絞り込み）。onSelect がなければ押せない（シナリオの全体像） */
  selected?: string | null
  onSelect?: (code: string) => void
}) {
  const { yen } = useChartFormat()
  const { fmt } = useAmountUnit()
  const { tip, props } = useMarks()
  const [ref, W] = useWidth<HTMLDivElement>()
  const parts = [
    { key: 'actual', label: '実績', color: actualColor, value: composition.get('actual') ?? 0n, level: false },
    ...levels.map((l, i) => ({ key: l.code, label: `${l.code} ${l.name}`, color: levelRamp[Math.min(i, levelRamp.length - 1)], value: composition.get(l.code) ?? 0n, level: true })),
  ].filter((p) => p.value > 0n)
  const downside = composition.get('downside') ?? 0n
  const total = parts.reduce((s, p) => s + p.value, 0n)
  const H = 34
  let x = 0
  return (
    <div>
      <div className="mb-1.5 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600" aria-label="凡例">
        {parts.map((p) => (
          <Swatch key={p.key} shape="square" color={p.color} label={p.label} />
        ))}
        {downside !== 0n && <Swatch shape="square" color={downsideColor} label={`ダウンサイド ${fmt(downside)}`} />}
      </div>
      {total === 0n ? (
        <p className="text-xs text-slate-500">売上の見込がありません。</p>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="group" aria-label="売上の確度の構成">
            {parts.map((p) => {
              const w = (Number(p.value) / Number(total)) * W
              const at = x
              x += w
              const dim = selected != null && selected !== p.key
              return (
                <g
                  key={p.key}
                  {...props(p.key, p.level && onSelect ? () => onSelect(p.key) : null, `${p.label}: ${yen(toNum(p.value))}`, (
                    <TipBody
                      title={p.label}
                      rows={[
                        ['売上', yen(toNum(p.value))],
                        ['割合', `${((Number(p.value) / Number(total)) * 100).toFixed(1)}%`],
                      ]}
                      link={false}
                      hint={p.level && onSelect ? (selected === p.key ? 'クリックで絞り込みを外す' : 'クリックでこの段階の施策に絞り込む') : undefined}
                    />
                  ))}
                >
                  <rect x={at + 1} y={2} width={Math.max(0, w - 2)} height={H - 4} rx={4} fill={p.color} opacity={dim ? 0.35 : 1} />
                </g>
              )
            })}
          </svg>
          <Tooltip tip={tip} />
        </div>
      )}
    </div>
  )
}

/** 更新の進み具合（完了・入力中・未着手の施策の数） */
export function ProgressBar({ counts }: { counts: { completed: number; in_progress: number; not_started: number } }) {
  const total = counts.completed + counts.in_progress + counts.not_started
  const parts = [
    { key: 'completed', label: '完了', value: counts.completed, className: 'bg-emerald-600' },
    { key: 'in_progress', label: '入力中', value: counts.in_progress, className: 'bg-amber-400' },
    { key: 'not_started', label: '未着手', value: counts.not_started, className: 'bg-slate-300' },
  ]
  if (total === 0) return <p className="text-xs text-slate-500">更新の対象の施策はありません。</p>
  return (
    <div>
      <div className="flex h-3 gap-0.5 overflow-hidden rounded-full" role="img" aria-label={parts.map((p) => `${p.label} ${p.value}件`).join('、')}>
        {parts.map((p) => p.value > 0 && <div key={p.key} className={cx('h-full first:rounded-l-full last:rounded-r-full', p.className)} style={{ width: `${(p.value / total) * 100}%` }} />)}
      </div>
      <dl className="mt-1.5 flex flex-wrap gap-x-4 text-xs text-slate-600">
        {parts.map((p) => (
          <div key={p.key} className="flex items-center gap-1.5">
            <span className={cx('inline-block size-2.5 rounded-full', p.className)} />
            <dt>{p.label}</dt>
            <dd className="font-medium text-slate-900 tabular-nums">{p.value}件</dd>
          </div>
        ))}
        <div className="text-slate-500">完了 {Math.round((counts.completed / total) * 100)}%</div>
      </dl>
    </div>
  )
}
