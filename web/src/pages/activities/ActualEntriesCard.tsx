import { useState } from 'react'
import { api, download, query } from '../../api/client'
import { allocatedByLabels, type ActualEntries, type ActualEntry, type List, type Subject } from '../../api/types'
import { Button, Card, Empty, ErrorMessage, Loading, Select, Table } from '../../components/ui'
import { formatYen, yearMonthLabel } from '../../lib/format'
import { useApi } from '../../lib/useApi'
import { RestrictedNote } from '../../components/RestrictedNote'
import { Help } from '../../components/Help'

/** 月（YYYY-MM）の年度（4月開始） */
function fiscalYearOf(ym: string): number {
  const y = Number(ym.slice(0, 4))
  return Number(ym.slice(5, 7)) >= 4 ? y : y - 1
}

/**
 * 施策の実績の明細（docs/plan.md「2.12」の実績の明細）。既定はすべての月で、新しい月から 100 件ずつ表示する。
 * 年度・月・科目で絞り込め、絞り込みどおりの明細を CSV で出力できる。合計は絞り込んだ全件。
 * 明細を見せない会計科目（給与など）は、FP&A 以外には会計科目ごとの合計だけを表示する。
 */
export function ActualEntriesCard({ activityId }: { activityId: number }) {
  const [fiscalYear, setFiscalYear] = useState('')
  const [month, setMonth] = useState('')
  const [subjectId, setSubjectId] = useState('')
  const filter = query({ fiscal_year: fiscalYear || undefined, month: month || undefined, subject_id: subjectId || undefined })
  const base = `/activities/${activityId}/actual-entries`
  const entries = useApi<ActualEntries>(`${base}${filter}`)
  const subjects = useApi<List<Subject>>('/subjects')
  const subjectName = new Map((subjects.data?.items ?? []).map((s) => [s.id, s.name]))
  // 「さらに表示」で読み足した明細（絞り込みが変わったら捨てる）
  const [more, setMore] = useState<{ filter: string; items: ActualEntry[]; hasMore: boolean } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const data = entries.data
  const extra = more?.filter === filter ? more : null
  const items = [...(data?.items ?? []), ...(extra?.items ?? [])]
  const hasMore = extra ? extra.hasMore : (data?.has_more ?? false)

  const loadMore = async () => {
    setBusy(true)
    setError(null)
    try {
      const sep = filter ? '&' : '?'
      const res = await api.get<ActualEntries>(`${base}${filter}${sep}offset=${items.length}`)
      setMore({ filter, items: [...(extra?.items ?? []), ...res.items], hasMore: res.has_more })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  const exportCsv = async () => {
    setError(null)
    try {
      await download(`${base}/export${filter}`)
    } catch (err) {
      setError(err)
    }
  }

  const months = data?.months ?? []
  const years = [...new Set(months.map(fiscalYearOf))].sort((a, b) => b - a)
  const shownMonths = [...months].reverse().filter((m) => !fiscalYear || fiscalYearOf(m) === Number(fiscalYear))
  const subjectIds = [...new Set([...items.map((e) => e.subject_id), ...(data?.hidden ?? []).map((h) => h.subject_id)])]
  const selectClass = 'py-1 text-xs'

  return (
    <Card
      title="実績の明細"
      actions={
        months.length > 0 ? (
          <Button size="sm" onClick={exportCsv}>
            CSV に出力
          </Button>
        ) : undefined
      }
    >
      <RestrictedNote hidden={data?.restricted_hidden} className="mb-3" />
      {months.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Select aria-label="年度" value={fiscalYear} onChange={(e) => {
              setFiscalYear(e.target.value)
              setMonth('')
            }} className={`w-32 ${selectClass}`}>
            <option value="">すべての年度</option>
            {years.map((y) => (
              <option key={y} value={y}>
                {y}年度
              </option>
            ))}
          </Select>
          <Select aria-label="月" value={month} onChange={(e) => setMonth(e.target.value)} className={`w-36 ${selectClass}`}>
            <option value="">すべての月</option>
            {shownMonths.map((m) => (
              <option key={m} value={m}>
                {yearMonthLabel(m)}
              </option>
            ))}
          </Select>
          <Select aria-label="科目" value={subjectId} onChange={(e) => setSubjectId(e.target.value)} className={`w-40 ${selectClass}`}>
            <option value="">すべての科目</option>
            {(subjects.data?.items ?? [])
              .filter((s) => subjectIds.includes(s.id) || String(s.id) === subjectId)
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
          </Select>
        </div>
      )}
      {entries.error ? (
        <ErrorMessage error={entries.error} />
      ) : !data ? (
        <Loading />
      ) : months.length === 0 ? (
        <Empty>この施策に割り当てられた実績はまだありません</Empty>
      ) : (
        <div className="space-y-2">
          {items.length === 0 && data.hidden.length === 0 ? (
            <p className="text-sm text-slate-500">条件に合う明細はありません（明細を取り込む前の実績は、明細がありません）。</p>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>月</th>
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
                {data.hidden.map((h) => (
                  <tr key={`${h.gl_account_code}-${h.subject_id}`} className="bg-slate-50">
                    <td className="text-xs whitespace-nowrap text-slate-500">{month ? yearMonthLabel(month) : '合計'}</td>
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
                {items.map((e) => (
                  <tr key={e.id}>
                    <td className="text-xs whitespace-nowrap text-slate-600">{yearMonthLabel(e.target_month)}</td>
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
              </tbody>
            </Table>
          )}
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-xs text-slate-500" role="status">
              {data.total.toLocaleString('ja-JP')} 件中 {items.length.toLocaleString('ja-JP')} 件を表示
            </span>
            {hasMore && (
              <Button size="sm" onClick={loadMore} disabled={busy}>
                {busy ? '読み込み中…' : 'さらに表示（100 件）'}
              </Button>
            )}
          </div>
          {error ? <ErrorMessage error={error} /> : null}
          <p className="text-xs text-slate-500">
            明細の合計 {formatYen(data.entries_total)} 円
            {data.entries_total !== data.fact_total && <span className="ml-1 text-amber-700">（実績データの合計 {formatYen(data.fact_total)} 円と一致しません。明細のない実績が含まれています）</span>}
            <Help>明細は最新の取込を表示します。ロック済みのシナリオの実績とは異なる場合があります。合計は絞り込んだすべての明細の合計です。</Help>
          </p>
        </div>
      )}
    </Card>
  )
}
