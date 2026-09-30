import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, ApiError } from '../../api/client'
import { calcModeLabels, categoryLabels, type List, type Scenario, type Subject, type ValuesView } from '../../api/types'
import { Badge, Button, Card, ErrorMessage, Loading, PageHeader, Select, Textarea, cx } from '../../components/ui'
import { formatNumber, formatPercent, formatYen, monthLabel, yearMonthLabel } from '../../lib/format'
import { Link, navigate } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { ScenarioBadges } from './ScenarioListPage'

/** 1セルの入力内容。value が '' なら削除 */
type CellEdit = { value: string; is_provisional: boolean; provisional_reason: string }

/** セルのキー。d = ドライバー値、a = 金額 */
type CellKey = `d:${number}:${string}` | `a:${number}:${string}`

type ServerCell = CellEdit & { source?: string }

const sourceLabels: Record<string, string> = { manual: '直接入力', formula: '計算式', import: '取込' }

function normalize(v: string): string {
  return v.replace(/,/g, '').trim()
}

/** シナリオ×施策の月別の数値を表示・入力する画面 */
export function ValuesPage({ scenarioId, activityId }: { scenarioId: string; activityId: string }) {
  const path = `/scenarios/${scenarioId}/activities/${activityId}`
  const view = useApi<ValuesView>(path)
  const subjects = useApi<List<Subject>>('/subjects')
  const scenarios = useApi<List<Scenario>>('/scenarios')

  const [edits, setEdits] = useState<Map<CellKey, CellEdit>>(new Map())
  const [extraSubjects, setExtraSubjects] = useState<number[]>([])
  const [selected, setSelected] = useState<CellKey | null>(null)
  const [reason, setReason] = useState('')
  const [saveError, setSaveError] = useState<unknown>(null)
  const [cellErrors, setCellErrors] = useState<Map<CellKey, string>>(new Map())
  const [saving, setSaving] = useState(false)
  const [savedMessage, setSavedMessage] = useState('')

  const v = view.data

  // サーバーの値を（キー → 値）で引けるようにする
  const server = useMemo(() => {
    const m = new Map<CellKey, ServerCell>()
    for (const d of v?.drivers ?? []) {
      for (const c of d.values) m.set(`d:${d.id}:${c.target_month}`, { value: String(c.value), is_provisional: c.is_provisional, provisional_reason: c.provisional_reason })
    }
    for (const a of v?.amounts ?? []) {
      for (const c of a.values) m.set(`a:${a.subject_id}:${c.target_month}`, { value: String(c.amount), is_provisional: c.is_provisional, provisional_reason: c.provisional_reason, source: c.source })
    }
    return m
  }, [v])

  const serverCell = (k: CellKey): CellEdit => server.get(k) ?? { value: '', is_provisional: false, provisional_reason: '' }
  const cell = (k: CellKey): CellEdit => edits.get(k) ?? serverCell(k)
  const isChanged = (k: CellKey) => {
    const e = edits.get(k)
    if (!e) return false
    const s = serverCell(k)
    return normalize(e.value) !== s.value || e.is_provisional !== s.is_provisional || (e.is_provisional && e.provisional_reason !== s.provisional_reason)
  }
  const changedKeys = [...edits.keys()].filter(isChanged)
  const dirty = changedKeys.length > 0

  // 未保存の変更があるときは、ページを離れる前に確認する
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const error = view.error ?? subjects.error ?? scenarios.error
  if (error) return <ErrorMessage error={error} />
  if (!v || !subjects.data || !scenarios.data) return <Loading />

  const editable = v.editable
  const update = (k: CellKey, patch: Partial<CellEdit>) => {
    setEdits((prev) => new Map(prev).set(k, { ...cell(k), ...patch }))
    setSavedMessage('')
  }

  // 金額の行: 既存の行＋追加した行。収益 → 費用の順
  const subjectById = new Map(subjects.data.items.map((s) => [s.id, s]))
  const amountRows = [
    ...v.amounts,
    ...extraSubjects
      .filter((id) => !v.amounts.some((a) => a.subject_id === id))
      .map((id) => {
        const s = subjectById.get(id)!
        return { subject_id: id, code: s.code, name: s.name, category: s.category, has_formula: false, values: [] }
      }),
  ].sort((a, b) => (a.category === b.category ? 0 : a.category === 'revenue' ? -1 : 1))
  const isFormulaRow = (row: { has_formula: boolean }) => row.has_formula && v.activity.calc_mode === 'formula'
  const addableSubjects = subjects.data.items.filter((s) => !amountRows.some((r) => r.subject_id === s.id))

  const monthTotal = (category: 'revenue' | 'expense', month: string) => {
    let sum = 0n
    for (const r of amountRows) {
      if (r.category !== category) continue
      const raw = normalize(cell(`a:${r.subject_id}:${month}`).value)
      if (/^-?\d+$/.test(raw)) sum += BigInt(raw)
    }
    return sum
  }
  const rowTotal = (subjectId: number) => {
    let sum = 0n
    for (const m of v.months) {
      const raw = normalize(cell(`a:${subjectId}:${m}`).value)
      if (/^-?\d+$/.test(raw)) sum += BigInt(raw)
    }
    return sum
  }

  const save = async () => {
    setSaving(true)
    setSaveError(null)
    setCellErrors(new Map())
    const driverKeys = changedKeys.filter((k) => k.startsWith('d:'))
    const amountKeys = changedKeys.filter((k) => k.startsWith('a:'))
    const toItem = (k: CellKey) => {
      const [, id, month] = k.split(':')
      const e = edits.get(k)!
      const value = normalize(e.value)
      return { id: Number(id), target_month: month, value: value === '' ? null : value, is_provisional: e.is_provisional, provisional_reason: e.is_provisional ? e.provisional_reason : '' }
    }
    let current: 'driver' | 'amount' = 'driver'
    try {
      let latest: ValuesView | undefined
      if (driverKeys.length > 0) {
        latest = await api.put<ValuesView>(`${path}/driver-values`, {
          reason,
          values: driverKeys.map(toItem).map(({ id, ...rest }) => ({ driver_id: id, ...rest })),
        })
        // ドライバー値は保存できたので、編集中の一覧から外す
        setEdits((prev) => {
          const next = new Map(prev)
          driverKeys.forEach((k) => next.delete(k))
          return next
        })
      }
      current = 'amount'
      if (amountKeys.length > 0) {
        latest = await api.put<ValuesView>(`${path}/amounts`, {
          reason,
          amounts: amountKeys.map(toItem).map(({ id, value, ...rest }) => ({ subject_id: id, amount: value, ...rest })),
        })
      }
      if (latest) view.setData(latest)
      setEdits(new Map())
      setExtraSubjects([])
      setReason('')
      setSavedMessage(`${changedKeys.length} 件を保存しました`)
    } catch (err) {
      setSaveError(err)
      if (err instanceof ApiError) {
        // "values[3].value" のようなエラーを、送ったセルに対応づける
        const keys = current === 'driver' ? driverKeys : amountKeys
        const m = new Map<CellKey, string>()
        for (const [field, msg] of Object.entries(err.details)) {
          const match = field.match(/^(?:values|amounts)\[(\d+)\]/)
          if (match && keys[Number(match[1])]) m.set(keys[Number(match[1])], msg)
        }
        setCellErrors(m)
      }
      if (current === 'amount') view.reload()
    } finally {
      setSaving(false)
    }
  }

  const discard = () => {
    setEdits(new Map())
    setExtraSubjects([])
    setCellErrors(new Map())
    setSaveError(null)
  }

  const selectedInfo = selected ? describeCell(selected, v, amountRows) : null
  const otherScenarios = scenarios.data.items.filter((s) => s.id !== v.scenario.id)

  return (
    <>
      <div className="mb-2 flex flex-wrap gap-4 text-sm">
        <Link to={`/scenarios/${v.scenario.id}`} className="text-slate-500 hover:text-slate-700">
          ← {v.scenario.name}
        </Link>
        <Link to={`/activities/${v.activity.id}`} className="text-slate-500 hover:text-slate-700">
          施策の詳細 →
        </Link>
      </div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-2">
            {v.activity.name}
            <span className="font-mono text-sm font-normal text-slate-500">{v.activity.code}</span>
          </span>
        }
        description={
          <span className="flex flex-wrap items-center gap-2">
            {v.scenario.name}
            <ScenarioBadges s={v.scenario} />
            <span>算出方式: {calcModeLabels[v.activity.calc_mode]}</span>
            <span>確度: {formatPercent(v.activity.probability)}</span>
          </span>
        }
        actions={
          <Select
            aria-label="シナリオを切り替える"
            value=""
            onChange={(e) => e.target.value && navigate(`/scenarios/${e.target.value}/activities/${v.activity.id}`)}
            className="w-56"
          >
            <option value="">別のシナリオで見る…</option>
            {otherScenarios.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        }
      />
      {!editable && <p className="mb-4 rounded-md bg-slate-100 px-4 py-2 text-sm text-slate-700">{readonlyReason(v)}</p>}

      <Card title="ドライバー・KPI" className="mb-4">
        {v.drivers.length === 0 ? (
          <p className="text-sm text-slate-500">
            ドライバーがありません。
            <Link to={`/activities/${v.activity.id}`} className="text-indigo-700 underline">
              施策の詳細
            </Link>
            で追加できます。
          </p>
        ) : (
          <Grid months={v.months}>
            {v.drivers.map((d) => (
              <tr key={d.id}>
                <RowHeader title={d.name} sub={`${d.code}${d.unit ? `（${d.unit}）` : ''}`} />
                {v.months.map((m) => {
                  const k: CellKey = `d:${d.id}:${m}`
                  return (
                    <CellInput
                      key={m}
                      label={`${d.name} ${monthLabel(m)}`}
                      value={cell(k).value}
                      display={formatNumber(cell(k).value)}
                      editable={editable}
                      provisional={cell(k).is_provisional}
                      changed={isChanged(k)}
                      error={cellErrors.get(k)}
                      selected={selected === k}
                      onFocus={() => setSelected(k)}
                      onChange={(value) => update(k, { value })}
                    />
                  )
                })}
                <td />
              </tr>
            ))}
          </Grid>
        )}
      </Card>

      <Card
        title="金額（円）"
        className="mb-4"
        actions={
          editable &&
          addableSubjects.length > 0 && (
            <Select aria-label="科目を追加" value="" onChange={(e) => e.target.value && setExtraSubjects((p) => [...p, Number(e.target.value)])} className="w-48 py-1 text-xs">
              <option value="">＋ 科目を追加…</option>
              {addableSubjects.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.code} {s.name}
                </option>
              ))}
            </Select>
          )
        }
      >
        {v.activity.calc_mode === 'formula' && (
          <p className="mb-3 text-xs text-slate-500">
            <span className="rounded bg-indigo-50 px-1 font-mono text-indigo-700">fx</span> の行は計算式で算出します。ドライバー値を保存すると再計算されます。
          </p>
        )}
        {amountRows.length === 0 ? (
          <p className="text-sm text-slate-500">金額はまだありません。{editable ? '「＋ 科目を追加」から入力する科目を選んでください。' : ''}</p>
        ) : (
          <Grid months={v.months} total>
            {amountRows.map((r) => {
              const formulaRow = isFormulaRow(r)
              return (
                <tr key={r.subject_id}>
                  <RowHeader
                    title={
                      <>
                        {formulaRow && <span className="mr-1 rounded bg-indigo-50 px-1 font-mono text-xs text-indigo-700">fx</span>}
                        {r.name}
                      </>
                    }
                    sub={`${r.code} ${categoryLabels[r.category]}`}
                  />
                  {v.months.map((m) => {
                    const k: CellKey = `a:${r.subject_id}:${m}`
                    return (
                      <CellInput
                        key={m}
                        label={`${r.name} ${monthLabel(m)}`}
                        value={cell(k).value}
                        display={formatYen(cell(k).value)}
                        editable={editable && !formulaRow}
                        provisional={cell(k).is_provisional}
                        changed={isChanged(k)}
                        error={cellErrors.get(k)}
                        selected={selected === k}
                        onFocus={() => setSelected(k)}
                        onChange={(value) => update(k, { value })}
                      />
                    )
                  })}
                  <td className="bg-slate-50 px-2 text-right font-medium tabular-nums">{formatYen(String(rowTotal(r.subject_id)))}</td>
                </tr>
              )
            })}
            {(['revenue', 'expense'] as const).map((c) => (
              <tr key={c} className="bg-slate-50 font-semibold">
                <th scope="row" className="sticky left-0 z-10 bg-slate-50 px-3 py-1.5 text-left text-xs text-slate-600">
                  {categoryLabels[c]}計
                </th>
                {v.months.map((m) => (
                  <td key={m} className="px-2 py-1.5 text-right text-xs tabular-nums">
                    {formatYen(String(monthTotal(c, m)))}
                  </td>
                ))}
                <td className="px-2 text-right text-xs tabular-nums">{formatYen(String(v.months.reduce((s, m) => s + monthTotal(c, m), 0n)))}</td>
              </tr>
            ))}
            <tr className="bg-slate-100 font-semibold">
              <th scope="row" className="sticky left-0 z-10 bg-slate-100 px-3 py-1.5 text-left text-xs text-slate-700">
                利益（収益 − 費用）
              </th>
              {v.months.map((m) => (
                <td key={m} className="px-2 py-1.5 text-right text-xs tabular-nums">
                  {formatYen(String(monthTotal('revenue', m) - monthTotal('expense', m)))}
                </td>
              ))}
              <td className="px-2 text-right text-xs tabular-nums">{formatYen(String(v.months.reduce((s, m) => s + monthTotal('revenue', m) - monthTotal('expense', m), 0n)))}</td>
            </tr>
          </Grid>
        )}
      </Card>

      {selected && selectedInfo && (
        <Card title={`選択中のセル: ${selectedInfo.label}（${yearMonthLabel(selectedInfo.month)}）`} className="mb-4">
          <div className="flex flex-wrap items-start gap-6 text-sm">
            {selected.startsWith('a:') && server.get(selected)?.source && <div>登録方法: {sourceLabels[server.get(selected)!.source!]}</div>}
            {selectedInfo.editable && editable ? (
              <>
                <label className="flex items-center gap-2">
                  <input type="checkbox" className="size-4 rounded border-slate-300" checked={cell(selected).is_provisional} onChange={(e) => update(selected, { is_provisional: e.target.checked })} />
                  仮の値（確定していない値）
                </label>
                {cell(selected).is_provisional && (
                  <div className="min-w-72 flex-1">
                    <Textarea
                      aria-label="仮の値の理由"
                      value={cell(selected).provisional_reason}
                      onChange={(e) => update(selected, { provisional_reason: e.target.value })}
                      placeholder="仮の値の理由（例: 見積待ちのため前年実績で仮置き）"
                      className="min-h-12"
                    />
                  </div>
                )}
              </>
            ) : cell(selected).is_provisional ? (
              <div>
                <Badge tone="amber">仮の値</Badge> <span className="ml-1 text-slate-700">{cell(selected).provisional_reason}</span>
              </div>
            ) : (
              <div className="text-slate-500">確定値</div>
            )}
          </div>
          {cellErrors.get(selected) && <p className="mt-2 text-sm text-red-600">{cellErrors.get(selected)}</p>}
        </Card>
      )}

      <ConditionCard path={path} condition={v.condition} editable={editable} onSaved={(nv) => view.setData(nv)} />

      {editable && (dirty || saveError || savedMessage) ? (
        <div className="sticky bottom-0 z-20 -mx-4 mt-4 border-t border-slate-200 bg-white/95 px-4 py-3 shadow-[0_-4px_12px_rgba(0,0,0,0.05)] backdrop-blur">
          <div className="mx-auto flex max-w-7xl flex-wrap items-start gap-3">
            {dirty ? (
              <>
                <div className="pt-2 text-sm font-medium text-slate-700">{changedKeys.length} 件の変更</div>
                <Textarea
                  aria-label="変更理由"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  placeholder="変更理由（必須）例: 10月の受注確定を反映"
                  className={cx('min-h-10 flex-1', saveError instanceof ApiError && saveError.details.reason ? 'border-red-400' : '')}
                  rows={1}
                />
                <Button onClick={discard} disabled={saving}>
                  取り消し
                </Button>
                <Button variant="primary" onClick={save} disabled={saving || reason.trim() === ''}>
                  {saving ? '保存中…' : '保存'}
                </Button>
              </>
            ) : (
              savedMessage && <p className="py-2 text-sm text-emerald-700">{savedMessage}</p>
            )}
            {saveError ? (
              <div className="w-full">
                <ErrorMessage error={saveError} />
                {cellErrors.size > 0 && <p className="mt-1 text-xs text-red-600">赤枠のセルを選ぶと、エラーの内容を確認できます。</p>}
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
    </>
  )
}

function readonlyReason(v: ValuesView): string {
  if (v.scenario.is_locked) return 'このシナリオはロックされているため、参照のみです。'
  if (v.scenario.scenario_kind === 'actual') return '実績シナリオの数値は CSV の取込で登録します。画面からは参照のみです。'
  return 'この施策の編集権限がないため、参照のみです。'
}

function describeCell(k: CellKey, v: ValuesView, amountRows: { subject_id: number; name: string; has_formula: boolean }[]) {
  const [kind, id, month] = k.split(':')
  if (kind === 'd') {
    return { label: v.drivers.find((d) => d.id === Number(id))?.name ?? '', month, editable: true }
  }
  const row = amountRows.find((r) => r.subject_id === Number(id))
  return { label: row?.name ?? '', month, editable: !(row?.has_formula && v.activity.calc_mode === 'formula') }
}

function Grid({ months, total, children }: { months: string[]; total?: boolean; children: ReactNode }) {
  return (
    <div className="-mx-4 overflow-x-auto">
      <table className="min-w-full border-separate border-spacing-0 text-sm">
        <thead>
          <tr>
            <th className="sticky left-0 z-10 min-w-44 border-b border-slate-200 bg-white px-3 py-1.5 text-left text-xs font-semibold text-slate-500" />
            {months.map((m) => (
              <th key={m} className="border-b border-slate-200 px-2 py-1.5 text-right text-xs font-semibold whitespace-nowrap text-slate-500">
                {monthLabel(m)}
              </th>
            ))}
            <th className="border-b border-slate-200 bg-slate-50 px-2 py-1.5 text-right text-xs font-semibold text-slate-500">{total ? '年計' : ''}</th>
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

function RowHeader({ title, sub }: { title: ReactNode; sub: string }) {
  return (
    <th scope="row" className="sticky left-0 z-10 border-b border-slate-100 bg-white px-3 py-1 text-left font-normal">
      <div className="text-sm font-medium whitespace-nowrap text-slate-800">{title}</div>
      <div className="font-mono text-xs whitespace-nowrap text-slate-400">{sub}</div>
    </th>
  )
}

function CellInput({
  label,
  value,
  display,
  editable,
  provisional,
  changed,
  error,
  selected,
  onFocus,
  onChange,
}: {
  label: string
  value: string
  display: string
  editable: boolean
  provisional: boolean
  changed: boolean
  error?: string
  selected: boolean
  onFocus: () => void
  onChange: (v: string) => void
}) {
  const tone = cx(
    'w-28 rounded px-2 py-1 text-right tabular-nums',
    provisional && 'bg-amber-50',
    changed && 'ring-2 ring-indigo-300',
    error && 'ring-2 ring-red-400',
    selected && !changed && !error && 'ring-2 ring-slate-300',
  )
  return (
    <td className="border-b border-slate-100 px-1 py-1">
      {editable ? (
        <input
          aria-label={label}
          inputMode="decimal"
          value={value}
          onFocus={onFocus}
          onChange={(e) => onChange(e.target.value)}
          title={error ?? (provisional ? '仮の値' : undefined)}
          className={cx(tone, 'border border-slate-200 bg-white focus:border-indigo-400 focus:outline-none', provisional && 'bg-amber-50')}
        />
      ) : (
        <button type="button" aria-label={`${label}: ${display || '未入力'}`} onClick={onFocus} className={cx(tone, 'block text-slate-700 hover:bg-slate-50')}>
          {display || <span className="text-slate-300">—</span>}
        </button>
      )}
    </td>
  )
}

function ConditionCard({ path, condition, editable, onSaved }: { path: string; condition: string | null; editable: boolean; onSaved: (v: ValuesView) => void }) {
  const [text, setText] = useState(condition ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [saved, setSaved] = useState(false)
  const changed = text.trim() !== (condition ?? '')

  const save = async () => {
    setBusy(true)
    setError(null)
    try {
      onSaved(await api.put<ValuesView>(`${path}/condition`, { description: text }))
      setSaved(true)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="このシナリオでの想定条件" className="mb-4">
      <p className="mb-2 text-xs text-slate-500">楽観・悲観シナリオの想定内容や、その発生条件などを記録します。</p>
      {editable ? (
        <div className="space-y-2">
          <Textarea
            aria-label="想定条件"
            value={text}
            onChange={(e) => {
              setText(e.target.value)
              setSaved(false)
            }}
            placeholder="例: B社の追加発注（2件）が11月に確定した場合"
          />
          <div className="flex items-center gap-3">
            <Button size="sm" variant="primary" disabled={!changed || busy} onClick={save}>
              {busy ? '保存中…' : '想定条件を保存'}
            </Button>
            {saved && !changed && <span className="text-xs text-emerald-700">保存しました</span>}
          </div>
          <ErrorMessage error={error} />
        </div>
      ) : (
        <p className="text-sm whitespace-pre-wrap text-slate-700">{condition || <span className="text-slate-400">記録なし</span>}</p>
      )}
    </Card>
  )
}
