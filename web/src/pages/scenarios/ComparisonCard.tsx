import { useState } from 'react'
import type { AmountRow } from '../../api/types'
import { Card, cx } from '../../components/ui'
import { isFavorable, varianceRate } from '../../lib/aggregate'
import { formatYen } from '../../lib/format'
import { buildSeriesPl, grainLabels, periodsOf, sumOver, type Grain, type SeriesPlNode } from '../../lib/pl'

/** 比べる系列。rows が undefined のときは「未設定」や「読み込み中」として扱う */
export type CompareSeries = { label: string; rows: AmountRow[] | undefined; note?: string }

const grains: Grain[] = ['month', 'quarter', 'half', 'year']

/**
 * 今回の見込を、基準・前回見込と比べる（docs/plan.md「2.10」）。
 * current は画面で入力中の値（未保存の入力と保存前の試算を含む）なので、入力に合わせて差が動く。
 */
export function ComparisonCard({ months, current, compares }: { months: string[]; current: AmountRow[]; compares: CompareSeries[] }) {
  const [grain, setGrain] = useState<Grain>('quarter')
  const [open, setOpen] = useState<Set<string>>(new Set())
  const available = compares.filter((c) => c.rows)
  const nodes = buildSeriesPl([current, ...available.map((c) => c.rows!)])
  const periods = periodsOf(months, grain)

  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const visible: { node: SeriesPlNode; depth: number }[] = []
  const walk = (n: SeriesPlNode, depth: number) => {
    visible.push({ node: n, depth })
    if (open.has(n.id)) n.children.forEach((c) => walk(c, depth + 1))
  }
  nodes.forEach((n) => walk(n, 0))

  const controls = (
    <div role="group" aria-label="比較の単位" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
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
  )

  return (
    <Card title="目標・前回の見込との比較" className="mb-4" actions={controls}>
      <p className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-500">
        <span>今回: 入力中の値（保存前の入力・試算を含む）</span>
        {compares.map((c) => (
          <span key={c.label}>
            {c.label}: {c.note}
          </span>
        ))}
      </p>
      <div className="-mx-4 overflow-x-auto">
        <table aria-label="目標・前回の見込との比較" className="min-w-full border-separate border-spacing-0 text-sm">
          <thead>
            <tr>
              <th className="sticky left-0 z-10 min-w-40 border-b border-slate-200 bg-white px-3 py-1.5 text-left text-xs font-semibold text-slate-500">項目</th>
              <th className="border-b border-slate-200 px-2 py-1.5" />
              {periods.map((p) => (
                <th key={p.key} className={cx('min-w-24 border-b border-slate-200 px-3 py-1.5 text-right text-xs font-semibold whitespace-nowrap text-slate-500', p.key === 'total' && 'bg-slate-50')}>
                  {p.label}
                </th>
              ))}
            </tr>
          </thead>
          {visible.map(({ node, depth }) => {
            const sums = node.values.map((v) => periods.map((p) => sumOver(v, p.months)))
            const rows: { label: string; values: bigint[]; diffBase?: bigint[] }[] = [{ label: '今回', values: sums[0] }]
            available.forEach((c, i) => {
              rows.push({ label: c.label, values: sums[i + 1] })
              rows.push({ label: `${c.label}との差`, values: sums[0].map((x, j) => x - sums[i + 1][j]), diffBase: sums[i + 1] })
            })
            const strong = depth === 0
            const cellClass = (key: string) => cx('px-3 py-1 text-right tabular-nums whitespace-nowrap', key === 'total' && 'bg-slate-50')
            return (
              <tbody key={node.id}>
                {rows.map((row, ri) => {
                  const last = ri === rows.length - 1
                  return (
                    <tr key={row.label} className={cx(strong && 'font-semibold', node.id === 'profit' && 'bg-slate-50')}>
                      {ri === 0 && (
                        <th scope="rowgroup" rowSpan={rows.length} className={cx('sticky left-0 z-10 border-b border-slate-200 bg-white py-1 pr-3 text-left align-top', node.id === 'profit' && 'bg-slate-50')}>
                          <div className="flex items-start gap-1" style={{ paddingLeft: `${0.75 + depth * 1.25}rem` }}>
                            {node.children.length > 0 ? (
                              <button
                                type="button"
                                onClick={() => toggle(node.id)}
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
                      )}
                      <td className={cx('px-2 py-1 text-xs font-normal whitespace-nowrap text-slate-500', last && 'border-b border-slate-200')}>{row.label}</td>
                      {row.values.map((value, j) => {
                        const key = periods[j].key
                        if (!row.diffBase) {
                          return (
                            <td key={key} className={cx(cellClass(key), last && 'border-b border-slate-200', value === 0n ? 'text-slate-300' : ri === 0 ? 'text-slate-900' : 'text-slate-600')}>
                              {formatYen(String(value))}
                            </td>
                          )
                        }
                        const fav = isFavorable(value, node.measure)
                        const rate = varianceRate(row.diffBase[j], row.diffBase[j] + value)
                        return (
                          <td
                            key={key}
                            title={rate === null ? undefined : `${rate > 0 ? '+' : ''}${rate}%`}
                            className={cx(cellClass(key), last && 'border-b border-slate-200', fav === true && 'text-emerald-700', fav === false && 'text-red-600', fav === null && 'text-slate-300')}
                          >
                            {value > 0n ? '+' : ''}
                            {formatYen(String(value))}
                            {key === 'total' && rate !== null && (
                              <span className="ml-1 text-xs font-normal">
                                ({rate > 0 ? '+' : ''}
                                {rate}%)
                              </span>
                            )}
                          </td>
                        )
                      })}
                    </tr>
                  )
                })}
              </tbody>
            )
          })}
        </table>
      </div>
    </Card>
  )
}
