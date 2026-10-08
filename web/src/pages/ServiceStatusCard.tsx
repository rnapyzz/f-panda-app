import { useState } from 'react'
import { unitTypeLabels, type ActivityProgress, type ActivityProgressReport, type Unit } from '../api/types'
import { Card, cx } from '../components/ui'
import { formatYen } from '../lib/format'
import { sumTotals, totalsByUnit, type UnitTotals } from '../lib/home'
import { scenarioLabel } from '../lib/scenario'
import { Help } from '../components/Help'

type Measure = 'profit' | 'revenue'

/**
 * サービスの状況（docs/plan.md「2.11 マネージャーの動線」の ①）。
 * ユニットごとに、売上・利益の今回・期初計画・前回見込と差を並べる（修正計画があれば併記）。行を選ぶとそのユニットに絞り込む。
 */
export function ServiceStatusCard({
  report,
  items,
  units,
  selectedUnit,
  onSelectUnit,
}: {
  report: ActivityProgressReport
  items: ActivityProgress[]
  units: Unit[]
  selectedUnit: string
  onSelectUnit: (unitId: string) => void
}) {
  const [measure, setMeasure] = useState<Measure>('profit')
  const [serviceOnly, setServiceOnly] = useState(true)
  const unitById = new Map(units.map((u) => [u.id, u]))
  const has = { initial: report.initial !== null, revised: report.revised !== null, previous: report.previous !== null }
  const shown = items.filter((it) => !serviceOnly || unitById.get(it.unit_id)?.unit_type === 'service')
  const rows = totalsByUnit(shown, has).sort((a, b) => (unitById.get(a.unitId)?.name ?? '').localeCompare(unitById.get(b.unitId)?.name ?? ''))
  const total = sumTotals(rows, has)

  const value = (p: UnitTotals['current'] | null) => (p ? p[measure] : null)
  const amount = (v: bigint | null) => (v === null ? <span className="text-slate-300">—</span> : formatYen(String(v)))
  const diff = (cur: bigint, cmp: bigint | null) => {
    if (cmp === null) return <span className="text-slate-300">—</span>
    const d = cur - cmp
    return <span className={cx(d > 0n && 'text-emerald-700', d < 0n && 'text-red-600', d === 0n && 'text-slate-400')}>{`${d > 0n ? '+' : ''}${formatYen(String(d))}`}</span>
  }
  const columns: { key: 'initial' | 'revised' | 'previous'; label: string }[] = [
    { key: 'initial', label: '期初計画' },
    ...(has.revised ? [{ key: 'revised' as const, label: '修正計画' }] : []),
    { key: 'previous', label: '前回の見込' },
  ]

  const toggle = (
    <div className="flex flex-wrap items-center gap-2">
      <div role="group" aria-label="指標" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
        {(['profit', 'revenue'] as const).map((m) => (
          <button
            key={m}
            type="button"
            aria-pressed={measure === m}
            onClick={() => setMeasure(m)}
            className={cx('rounded px-2.5 py-1 text-xs font-medium', measure === m ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
          >
            {m === 'profit' ? '利益' : '売上'}
          </button>
        ))}
      </div>
      <label className="flex items-center gap-1.5 text-xs text-slate-600">
        <input type="checkbox" className="size-4 rounded border-slate-300" checked={serviceOnly} onChange={(e) => setServiceOnly(e.target.checked)} />
        {unitTypeLabels.service}のみ
      </label>
    </div>
  )

  const renderRow = (r: UnitTotals, isTotal: boolean) => {
    const selected = !isTotal && String(r.unitId) === selectedUnit
    const unit = unitById.get(r.unitId)
    return (
      <tr key={isTotal ? 'total' : r.unitId} className={cx(isTotal && 'bg-slate-50 font-semibold', selected && 'bg-indigo-50')}>
        <th scope="row" className="text-left font-medium whitespace-nowrap">
          {isTotal ? (
            '合計'
          ) : (
            <button
              type="button"
              onClick={() => onSelectUnit(selected ? '' : String(r.unitId))}
              aria-pressed={selected}
              className="text-indigo-700 hover:underline"
              title={selected ? '絞り込みを解除' : 'このユニットに絞り込む'}
            >
              {unit?.name ?? `ユニット #${r.unitId}`}
            </button>
          )}
        </th>
        <td className="text-right tabular-nums">{amount(value(r.current))}</td>
        {columns.map((c) => (
          <td key={c.key} className="text-right tabular-nums">
            <div>{amount(value(r[c.key]))}</div>
            <div className="text-xs">{diff(value(r.current)!, value(r[c.key]))}</div>
          </td>
        ))}
        <td className="text-right text-xs whitespace-nowrap text-slate-500 tabular-nums">
          {r.open > 0 ? `未完了 ${r.open} / ${r.count}` : `${r.count} 件すべて完了`}
        </td>
      </tr>
    )
  }

  return (
    <Card
      title={
        <>
          サービスの状況（年間）
          <Help manual="manager#service">
            今回: {scenarioLabel(report.scenario)} ／ 期初計画: {report.initial ? report.initial.name : '未設定'}
            {report.revised && <> ／ 修正計画: {report.revised.name}</>} ／ 前回の見込: {report.previous ? report.previous.name : '未設定'}。差は今回 − 各シナリオ。ユニット名を選ぶと、下の一覧を絞り込みます。
          </Help>
        </>
      }
      className="mb-4"
      actions={toggle}
    >
      {rows.length === 0 ? (
        <p className="text-sm text-slate-500">{serviceOnly ? 'サービスのユニットの施策はありません。' : '施策はありません。'}</p>
      ) : (
        <div className="-mx-4 overflow-x-auto px-4">
          <table aria-label="サービスの状況" className="min-w-full text-sm [&_td]:border-b [&_td]:border-slate-100 [&_td]:px-3 [&_td]:py-1.5 [&_th]:border-b [&_th]:border-slate-100 [&_th]:px-3 [&_th]:py-1.5">
            <thead>
              <tr className="text-xs text-slate-500">
                <th className="text-left font-semibold">ユニット</th>
                <th className="text-right font-semibold">今回</th>
                {columns.map((c) => (
                  <th key={c.key} className="text-right font-semibold">
                    {c.label}
                    <div className="font-normal">（差）</div>
                  </th>
                ))}
                <th className="text-right font-semibold">更新の状況</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => renderRow(r, false))}
              {rows.length > 1 && renderRow(total, true)}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}
