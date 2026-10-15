// シナリオの全体像の図（docs/plan.md「2.31」）。依存ライブラリは使わず SVG で描く。
// 金額の大きさは1色の横棒（区分ごとに色を変えない。色は区分の名前ではなく、黒字・赤字の向きだけに使う）。

import { ChartCard, Swatch, TipBody, Tooltip, clip, useChartFormat, useMarks, useWidth } from '../../components/chart'
import { Table, cx } from '../../components/ui'
import { niceTicks } from '../../lib/activityCharts'
import { downColor, ink, trend, upColor } from '../../lib/chartTheme'
import { useTableView } from '../dashboard/DashboardCharts'

const ROW_H = 28
const toNum = (v: bigint) => Number(v)
const labelWidth = (w: number) => Math.min(200, Math.round(w * 0.3))

export type AmountBar = { key: string; label: string; value: bigint; share?: number | null; target: number | (() => void) | null; hint?: string }

/**
 * 区分ごとの金額の横棒。kind = 'size' は売上など大きさ（1色、割合を添える）、'signed' は利益など（黒字は右・赤字は左）
 */
export function AmountBars({ title, bars, kind, footer }: { title: string; bars: AmountBar[]; kind: 'size' | 'signed'; footer?: string }) {
  const { man, yen, signedYen } = useChartFormat()
  const { tip, active, props } = useMarks()
  const { table, toggle } = useTableView()
  const [ref, W] = useWidth<HTMLDivElement>()
  const LABEL_W = labelWidth(W)
  const values = bars.map((b) => toNum(b.value))
  const xs = niceTicks(Math.min(0, ...values), Math.max(0, ...values), 4)
  const [x0, x1] = [xs[0], xs[xs.length - 1]]
  const top = 24
  const height = top + bars.length * ROW_H + 8
  const px = (v: number) => LABEL_W + ((v - x0) / (x1 - x0 || 1)) * (W - LABEL_W - 88)
  const fmt = kind === 'signed' ? signedYen : yen
  return (
    <ChartCard
      legend={
        kind === 'signed' ? (
          <>
            <Swatch shape="square" color={upColor} label="黒字" />
            <Swatch shape="square" color={downColor} label="赤字" />
          </>
        ) : (
          <span className="text-slate-500">{title}</span>
        )
      }
      actions={toggle}
      footer={footer}
    >
      {table ? (
        <Table>
          <thead>
            <tr>
              <th>区分</th>
              <th className="text-right">{title}</th>
              {kind === 'size' && <th className="text-right">割合</th>}
            </tr>
          </thead>
          <tbody>
            {bars.map((b) => (
              <tr key={b.key}>
                <td>{b.label}</td>
                <td className="text-right tabular-nums">{fmt(toNum(b.value))}</td>
                {kind === 'size' && <td className="text-right tabular-nums">{b.share == null ? '' : `${b.share}%`}</td>}
              </tr>
            ))}
          </tbody>
        </Table>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${height}`} className="h-auto w-full" role="group" aria-label={title}>
            {xs.map((v) => (
              <g key={v}>
                <line x1={px(v)} x2={px(v)} y1={top - 6} y2={height - 6} stroke={v === 0 ? ink.axis : ink.grid} strokeWidth={1} />
                <text x={px(v)} y={top - 10} textAnchor="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                  {man(v)}
                </text>
              </g>
            ))}
            {bars.map((b, i) => {
              const y = top + i * ROW_H + ROW_H / 2
              const v = toNum(b.value)
              const left = px(Math.min(0, v))
              const width = Math.max(2, Math.abs(px(v) - px(0)))
              const color = kind === 'size' ? trend.actual : v >= 0 ? upColor : downColor
              const text = `${kind === 'signed' ? man(v, true) : man(v)}${kind === 'size' && b.share != null ? `（${b.share}%）` : ''}`
              return (
                <g key={b.key} {...props(b.key, b.target, `${b.label}: ${fmt(v)}`, <TipBody title={b.label} rows={[[title, fmt(v)], ...(b.share != null ? ([['割合', `${b.share}%`]] as [string, string][]) : [])]} link={false} hint={b.hint} />)}>
                  <rect x={0} y={y - ROW_H / 2} width={W} height={ROW_H} rx={6} fill={active(b.key) ? '#f6f5ff' : 'transparent'} />
                  <text x={8} y={y} dominantBaseline="middle" fontSize={12} fill={ink.primary}>
                    {clip(b.label, LABEL_W - 20, 12)}
                  </text>
                  <rect x={left} y={y - 8} width={width} height={16} rx={4} fill={color} />
                  <text x={v >= 0 ? px(v) + 6 : px(0) + 6} y={y} dominantBaseline="middle" fontSize={11} fill={ink.secondary} className="tabular-nums">
                    {text}
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

export type StackPart = { key: string; label: string; color: string; value: bigint }

/** 売上の割合の帯（凡例・ツールチップ付き。区分の間に 2px のすき間） */
export function ShareStack({ title, parts }: { title: string; parts: StackPart[] }) {
  const { yen } = useChartFormat()
  const { tip, props } = useMarks()
  const [ref, W] = useWidth<HTMLDivElement>()
  const shown = parts.filter((p) => p.value > 0n)
  const total = shown.reduce((s, p) => s + p.value, 0n)
  const H = 30
  let x = 0
  return (
    <div>
      <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2">
        <h4 className="text-xs font-semibold text-slate-600">{title}</h4>
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-600" aria-label={`${title}の凡例`}>
          {shown.map((p) => (
            <Swatch key={p.key} shape="square" color={p.color} label={`${p.label} ${total > 0n ? ((Number(p.value) / Number(total)) * 100).toFixed(1) : '0'}%`} />
          ))}
        </div>
      </div>
      {total === 0n ? (
        <p className="text-xs text-slate-500">売上がありません。</p>
      ) : (
        <div className="relative" data-chart ref={ref}>
          <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="group" aria-label={title}>
            {shown.map((p) => {
              const w = (Number(p.value) / Number(total)) * W
              const at = x
              x += w
              return (
                <g key={p.key} {...props(p.key, null, `${p.label}: ${yen(toNum(p.value))}`, <TipBody title={p.label} rows={[['売上', yen(toNum(p.value))], ['割合', `${((Number(p.value) / Number(total)) * 100).toFixed(1)}%`]]} link={false} />)}>
                  <rect x={at + 1} y={2} width={Math.max(0, w - 2)} height={H - 4} rx={4} fill={p.color} />
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

/** 数字の小さなタイル（割合など） */
export function Stat({ label, value, hint, className }: { label: string; value: string; hint?: string; className?: string }) {
  return (
    <div className={cx('rounded-md border border-slate-200 bg-white px-3 py-2', className)}>
      <div className="text-xs text-slate-500">{label}</div>
      <div className="text-lg font-semibold text-slate-900 tabular-nums">{value}</div>
      {hint && <div className="text-xs text-slate-500">{hint}</div>}
    </div>
  )
}
