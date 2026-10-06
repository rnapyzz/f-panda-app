import { useState } from 'react'
import { allocatedByLabels, type ActualEntries, type List, type Subject } from '../../api/types'
import { Card, Empty, ErrorMessage, Loading, Select, Table } from '../../components/ui'
import { formatYen, yearMonthLabel } from '../../lib/format'
import { useApi } from '../../lib/useApi'

/**
 * 施策の実績の明細（docs/plan.md「2.12」の実績の明細）。月を選ぶと、施策に割り当てた会計の明細を表示する。
 * 明細を見せない会計科目（給与など）は、FP&A 以外には会計科目ごとの合計だけを表示する。
 */
export function ActualEntriesCard({ activityId }: { activityId: number }) {
  const [month, setMonth] = useState('')
  const entries = useApi<ActualEntries>(`/activities/${activityId}/actual-entries${month ? `?month=${month}` : ''}`)
  const subjects = useApi<List<Subject>>('/subjects')
  const subjectName = new Map((subjects.data?.items ?? []).map((s) => [s.id, s.name]))
  const data = entries.data

  return (
    <Card
      title="実績の明細"
      actions={
        data && data.months.length > 0 ? (
          <div className="w-36">
            <Select aria-label="実績の月" value={data.month} onChange={(e) => setMonth(e.target.value)} className="py-1 text-xs">
              {[...data.months].reverse().map((m) => (
                <option key={m} value={m}>
                  {yearMonthLabel(m)}
                </option>
              ))}
            </Select>
          </div>
        ) : undefined
      }
    >
      {entries.error ? (
        <ErrorMessage error={entries.error} />
      ) : !data ? (
        <Loading />
      ) : data.months.length === 0 ? (
        <Empty>この施策に割り当てられた実績はまだありません</Empty>
      ) : (
        <div className="space-y-2">
          {data.items.length === 0 && data.hidden.length === 0 ? (
            <p className="text-sm text-slate-500">この月の実績には明細がありません（明細を取り込む前の実績です）。</p>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>科目</th>
                  <th>会計科目</th>
                  <th>部門</th>
                  <th>箱の ID</th>
                  <th>摘要</th>
                  <th className="text-right">金額</th>
                  <th>割当の根拠</th>
                </tr>
              </thead>
              <tbody>
                {data.items.map((e) => (
                  <tr key={e.id}>
                    <td className="whitespace-nowrap">{subjectName.get(e.subject_id)}</td>
                    <td className="whitespace-nowrap">
                      <span className="font-mono text-xs text-slate-500">{e.gl_account_code}</span> {e.gl_account_name}
                    </td>
                    <td className="font-mono text-xs">{e.department_code ?? '—'}</td>
                    <td className="font-mono text-xs">{e.box_code ?? '—'}</td>
                    <td className="text-sm text-slate-600">{e.description}</td>
                    <td className="text-right tabular-nums">{formatYen(e.amount)}</td>
                    <td className="text-xs whitespace-nowrap text-slate-500">{allocatedByLabels[e.allocated_by]}</td>
                  </tr>
                ))}
                {data.hidden.map((h) => (
                  <tr key={`${h.gl_account_code}-${h.subject_id}`} className="bg-slate-50">
                    <td className="whitespace-nowrap">{subjectName.get(h.subject_id)}</td>
                    <td className="whitespace-nowrap">
                      <span className="font-mono text-xs text-slate-500">{h.gl_account_code}</span> {h.gl_account_name}
                    </td>
                    <td colSpan={3} className="text-xs text-slate-500">
                      明細は FP&A のみが見られます（{h.count} 行の合計）
                    </td>
                    <td className="text-right tabular-nums">{formatYen(h.amount)}</td>
                    <td />
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
          <p className="text-xs text-slate-500">
            明細の合計 {formatYen(data.entries_total)} 円
            {data.entries_total !== data.fact_total && <span className="ml-1 text-amber-700">（実績データの合計 {formatYen(data.fact_total)} 円と一致しません。明細のない実績が含まれています）</span>}
            。明細は最新の取込を表示します。ロック済みのシナリオの実績とは異なる場合があります。
          </p>
        </div>
      )}
    </Card>
  )
}
