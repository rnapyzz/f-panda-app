import type { DriftMonth } from '../../api/types'
import { Table } from '../../components/ui'
import { formatYen, yearMonthLabel } from '../../lib/format'

function signed(v: string) {
  if (v === '0') return <span className="text-slate-300">—</span>
  return `${v.startsWith('-') ? '' : '+'}${formatYen(v)}`
}

/** ロック済みのシナリオの実績と今の実績の食い違い（月ごと）。差は「今の実績 − シナリオに保存した実績」 */
export function DriftTable({ months }: { months: DriftMonth[] }) {
  return (
    <Table>
      <thead>
        <tr>
          <th>月</th>
          <th className="text-right">収益の差</th>
          <th className="text-right">費用の差</th>
          <th className="text-right">違う件数</th>
        </tr>
      </thead>
      <tbody>
        {months.map((m) => (
          <tr key={m.month}>
            <td>{yearMonthLabel(m.month)}</td>
            <td className="text-right tabular-nums">{signed(m.revenue)}</td>
            <td className="text-right tabular-nums">{signed(m.expense)}</td>
            <td className="text-right tabular-nums" title="施策 × 科目で金額が違う件数（施策の間の付け替えだけなら、差は 0 でも件数が出ます）">
              {m.changed}
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  )
}
