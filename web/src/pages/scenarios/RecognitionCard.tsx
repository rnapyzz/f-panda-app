import { useState } from 'react'
import { api } from '../../api/client'
import type { ValuesView } from '../../api/types'
import { Button, Card, ErrorMessage, Select } from '../../components/ui'
import { buildWip, monthLabel, wipNotices } from '../../lib/wip'

/**
 * 検収月に一括の施策の計上月（docs/plan.md「2.28」）。選ぶとすぐに保存し、仕掛品振替を計算し直す。
 * 入力中の値があるときは、保存してから変える（振替は保存した原価から計算するため）。
 */
export function RecognitionCard({
  path,
  v,
  editable,
  dirty,
  currentYearMonth,
  onSaved,
}: {
  path: string
  v: ValuesView
  editable: boolean
  dirty: boolean
  /** 翌期の計画のとき、今回の見込（当期）の計上月 */
  currentYearMonth?: string | null
  onSaved: (view: ValuesView) => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const wip = v.wip
  if (!wip) return null
  const month = wip.recognition_month
  const actual = new Set(v.actual_months)
  const options = [...v.months, ...v.beyond_months].filter((m) => !actual.has(m))
  const table = buildWip(v)
  const notices = table ? wipNotices(table, month) : []

  const change = async (next: string | null) => {
    setBusy(true)
    setError(null)
    try {
      onSaved(await api.put<ValuesView>(`${path}/recognition`, { recognition_month: next }))
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="計上月（検収月に一括）" className="mb-4">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          <span className="text-slate-600">計上月</span>
          <Select
            aria-label="計上月"
            value={month ?? ''}
            disabled={!editable || busy || dirty}
            onChange={(e) => change(e.target.value || null)}
            className="w-40 py-1 text-sm"
          >
            <option value="">未設定</option>
            {month && !options.includes(month) && <option value={month}>{monthLabel(month)}（実績の月）</option>}
            {options.map((m) => (
              <option key={m} value={m}>
                {monthLabel(m)}
              </option>
            ))}
          </Select>
        </label>
        {editable && dirty && <span className="text-xs text-slate-500">入力中の値を保存してから変えられます</span>}
        {editable && !dirty && currentYearMonth && currentYearMonth !== month && options.includes(currentYearMonth) && (
          <Button size="sm" disabled={busy} onClick={() => change(currentYearMonth)}>
            今回の見込と同じ {monthLabel(currentYearMonth)} にする
          </Button>
        )}
      </div>
      <p className="mt-2 text-xs text-slate-500">
        原価は発生した月に入れます。計上月より前の月の原価は仕掛品に積み（仕掛品振替）、計上月に売上原価として戻します。仕掛品振替の行はアプリが計算します。
      </p>
      {wip.transfer_subject_id == null && <p className="mt-2 text-xs text-amber-700">仕掛品振替の科目が指定されていません。FP&A が「勘定科目」で指定すると、振替を計算します。</p>}
      {notices.length > 0 && (
        <ul className="mt-2 space-y-1 text-xs text-amber-800" aria-label="計上月の知らせ">
          {notices.map((n) => (
            <li key={n}>⚠ {n}</li>
          ))}
        </ul>
      )}
      <ErrorMessage error={error} />
    </Card>
  )
}
