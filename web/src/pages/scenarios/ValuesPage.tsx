import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api, ApiError } from '../../api/client'
import { categoryLabels, type AmountRow, type List, type Scenario, type Subject, type ValuesView } from '../../api/types'
import { SheetCell, SheetFrame, useSheet, type Sheet } from '../../components/Sheet'
import { Badge, Button, Card, ErrorMessage, Loading, PageHeader, Select, Textarea, cx } from '../../components/ui'
import { formatNumber, formatPercent, formatYen, monthLabel, yearMonthLabel } from '../../lib/format'
import { useActiveScenario } from '../../lib/activeScenario'
import { Link, navigate } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { ScenarioBadges } from './ScenarioListPage'

/** 1セルの入力内容。value が '' なら削除 */
type CellEdit = { value: string; is_provisional: boolean; provisional_reason: string }

/** セルのキー。d = ドライバー値（d:ドライバー:月）、a = 金額（a:科目:内訳:月。内訳 0 は科目への直接入力） */
type CellKey = `d:${number}:${string}` | `a:${number}:${number}:${string}`

type ServerCell = CellEdit & { source?: string }

const sourceLabels: Record<string, string> = { manual: '直接入力', formula: '計算式', import: '取込', actual: '実績' }

function normalize(v: string): string {
  return v.replace(/,/g, '').trim()
}

/** シナリオ×施策の月別の数値を表示・入力する画面 */
export function ValuesPage({ scenarioId, activityId }: { scenarioId: string; activityId: string }) {
  const path = `/scenarios/${scenarioId}/activities/${activityId}`
  const view = useApi<ValuesView>(path)
  const subjects = useApi<List<Subject>>('/subjects')
  const scenarios = useApi<List<Scenario>>('/scenarios')

  const error = view.error ?? subjects.error ?? scenarios.error
  if (error) return <ErrorMessage error={error} />
  if (!view.data || !subjects.data || !scenarios.data) return <Loading />
  return (
    <ValuesEditor
      key={path}
      path={path}
      v={view.data}
      subjects={subjects.data.items}
      scenarios={scenarios.data.items}
      setData={view.setData}
      reload={view.reload}
    />
  )
}

/** 金額の行（スプレッドシートの1行）。lineId 0 は科目への直接入力 */
type SheetLine = { row: AmountRow; lineId: number; label: string; title: ReactNode; sub: string; editable: boolean; indent: boolean }

function ValuesEditor({
  path,
  v,
  subjects,
  scenarios,
  setData,
  reload,
}: {
  path: string
  v: ValuesView
  subjects: Subject[]
  scenarios: Scenario[]
  setData: (v: ValuesView) => void
  reload: () => void
}) {
  const { active } = useActiveScenario()
  const [edits, setEdits] = useState<Map<CellKey, CellEdit>>(new Map())
  const [extraSubjects, setExtraSubjects] = useState<number[]>([])
  const [selected, setSelected] = useState<CellKey | null>(null)
  const [reason, setReason] = useState('')
  const [saveError, setSaveError] = useState<unknown>(null)
  const [cellErrors, setCellErrors] = useState<Map<CellKey, string>>(new Map())
  const [saving, setSaving] = useState(false)
  const [savedMessage, setSavedMessage] = useState('')

  // 元に戻す・やり直すための、編集内容の履歴
  const past = useRef<Map<CellKey, CellEdit>[]>([])
  const future = useRef<Map<CellKey, CellEdit>[]>([])

  // サーバーの値を（キー → 値）で引けるようにする
  const server = useMemo(() => {
    const m = new Map<CellKey, ServerCell>()
    for (const d of v.drivers) {
      for (const c of d.values) m.set(`d:${d.id}:${c.target_month}`, { value: String(c.value), is_provisional: c.is_provisional, provisional_reason: c.provisional_reason })
    }
    for (const a of v.amounts) {
      for (const [lineId, values] of [[0, a.values] as const, ...a.lines.map((l) => [l.id, l.values] as const)]) {
        for (const c of values) {
          m.set(`a:${a.subject_id}:${lineId}:${c.target_month}`, { value: String(c.amount), is_provisional: c.is_provisional, provisional_reason: c.provisional_reason, source: c.source })
        }
      }
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

  const editable = v.editable
  // 決算確定月以前の月は実績。入力できない
  const actualMonths = new Set(v.actual_months)
  const monthOf = (k: CellKey) => k.slice(k.lastIndexOf(':') + 1)
  const update = (k: CellKey, patch: Partial<CellEdit>) => {
    setEdits((prev) => new Map(prev).set(k, { ...cell(k), ...patch }))
    setSavedMessage('')
  }
  /** グリッドからの値の変更（まとめて1回の操作として、元に戻せるようにする） */
  const setValues = (entries: [CellKey, string][]) => {
    const next = new Map(edits)
    for (const [k, value] of entries) next.set(k, { ...cell(k), value })
    past.current = [...past.current.slice(-99), edits]
    future.current = []
    setEdits(next)
    setSavedMessage('')
  }
  const undo = () => {
    const prev = past.current.pop()
    if (!prev) return
    future.current.push(edits)
    setEdits(prev)
  }
  const redo = () => {
    const next = future.current.pop()
    if (!next) return
    past.current.push(edits)
    setEdits(next)
  }

  // 金額の行: 既存の行＋追加した行。収益 → 費用の順
  const subjectById = new Map(subjects.map((s) => [s.id, s]))
  const amountRows: AmountRow[] = [
    ...v.amounts,
    ...extraSubjects
      .filter((id) => !v.amounts.some((a) => a.subject_id === id))
      .map((id) => {
        const s = subjectById.get(id)!
        return { subject_id: id, code: s.code, name: s.name, category: s.category, values: [], lines: [] }
      }),
  ].sort((a, b) => (a.category === b.category ? 0 : a.category === 'revenue' ? -1 : 1))
  const addableSubjects = subjects.filter((s) => !amountRows.some((r) => r.subject_id === s.id))
  const hasFormulaLines = amountRows.some((r) => r.lines.some((l) => l.formula_enabled))

  /** 科目の月の金額（内訳と科目への直接入力の合計） */
  const subjectMonth = (r: AmountRow, month: string) => [0, ...r.lines.map((l) => l.id)].reduce((sum, lineId) => sum + toBigInt(cell(`a:${r.subject_id}:${lineId}:${month}`).value), 0n)
  const monthTotal = (category: 'revenue' | 'expense', month: string) => amountRows.filter((r) => r.category === category).reduce((sum, r) => sum + subjectMonth(r, month), 0n)
  const rowTotal = (subjectId: number, lineId: number) => v.months.reduce((sum, m) => sum + toBigInt(cell(`a:${subjectId}:${lineId}:${m}`).value), 0n)
  const subjectTotal = (r: AmountRow) => v.months.reduce((sum, m) => sum + subjectMonth(r, m), 0n)

  // スプレッドシートの行。内訳のない科目は直接入力の1行、内訳のある科目は内訳の行と「その他」の行
  const sheetLines: SheetLine[] = amountRows.flatMap((r): SheetLine[] => {
    const sub = `${r.code} ${categoryLabels[r.category]}`
    if (r.lines.length === 0) return [{ row: r, lineId: 0, label: r.name, title: r.name, sub, editable: true, indent: false }]
    return [
      ...r.lines.map((l) => ({
        row: r,
        lineId: l.id,
        label: `${r.name} ${l.name}`,
        title: (
          <>
            {l.formula_enabled && <span className="mr-1 rounded bg-indigo-50 px-1 font-mono text-xs text-indigo-700">fx</span>}
            {l.name}
          </>
        ),
        sub: l.formula_enabled ? l.expression : '直接入力',
        editable: !l.formula_enabled,
        indent: true,
      })),
      { row: r, lineId: 0, label: `${r.name} その他`, title: 'その他', sub: '科目への直接入力', editable: true, indent: true },
    ]
  })
  const readonlyKeys = new Set<CellKey>(sheetLines.filter((l) => !l.editable).flatMap((l) => v.months.map((m) => `a:${l.row.subject_id}:${l.lineId}:${m}` as CellKey)))
  const sheetOptions = {
    isEditable: (k: CellKey) => editable && !readonlyKeys.has(k) && !actualMonths.has(monthOf(k)),
    rawValue: (k: CellKey) => normalize(cell(k).value),
    setValues,
    onActivate: (k: CellKey) => setSelected(k),
    onUndo: undo,
    onRedo: redo,
  }
  const driverSheet = useSheet<CellKey>({ ...sheetOptions, grid: v.drivers.map((d) => v.months.map((m) => `d:${d.id}:${m}` as CellKey)) })
  const amountSheet = useSheet<CellKey>({ ...sheetOptions, grid: sheetLines.map((l) => v.months.map((m) => `a:${l.row.subject_id}:${l.lineId}:${m}` as CellKey)) })

  /** 金額を入力・表示する1行 */
  const amountLine = (l: SheetLine, index: number) => (
    <tr key={`${l.row.subject_id}:${l.lineId}`}>
      <RowHeader title={l.title} sub={l.sub} indent={l.indent} />
      {v.months.map((m, c) => {
        const k: CellKey = `a:${l.row.subject_id}:${l.lineId}:${m}`
        return (
          <SheetCell
            key={m}
            sheet={amountSheet}
            r={index}
            c={c}
            label={`${l.label} ${monthLabel(m)}`}
            display={formatYen(cell(k).value)}
            editable={editable && l.editable && !actualMonths.has(m)}
            provisional={cell(k).is_provisional}
            changed={isChanged(k)}
            error={cellErrors.get(k)}
          />
        )
      })}
      <td className="border-b border-slate-100 bg-slate-50 px-2 text-right tabular-nums">{formatYen(String(rowTotal(l.row.subject_id, l.lineId)))}</td>
    </tr>
  )

  const save = async () => {
    setSaving(true)
    setSaveError(null)
    setCellErrors(new Map())
    const driverKeys = changedKeys.filter((k) => k.startsWith('d:'))
    const amountKeys = changedKeys.filter((k) => k.startsWith('a:'))
    const toItem = (k: CellKey) => {
      const parts = k.split(':')
      const month = parts[parts.length - 1]
      const e = edits.get(k)!
      const value = normalize(e.value)
      return {
        id: Number(parts[1]),
        lineId: parts.length === 4 ? Number(parts[2]) : 0,
        target_month: month,
        value: value === '' ? null : value,
        is_provisional: e.is_provisional,
        provisional_reason: e.is_provisional ? e.provisional_reason : '',
      }
    }
    let current: 'driver' | 'amount' = 'driver'
    try {
      let latest: ValuesView | undefined
      if (driverKeys.length > 0) {
        latest = await api.put<ValuesView>(`${path}/driver-values`, {
          reason,
          values: driverKeys.map(toItem).map(({ id, target_month, value, is_provisional, provisional_reason }) => ({ driver_id: id, target_month, value, is_provisional, provisional_reason })),
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
          amounts: amountKeys.map(toItem).map(({ id, lineId, value, ...rest }) => ({ subject_id: id, line_id: lineId || null, amount: value, ...rest })),
        })
      }
      if (latest) setData(latest)
      setEdits(new Map())
      past.current = []
      future.current = []
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
      if (current === 'amount') reload()
    } finally {
      setSaving(false)
    }
  }

  const discard = () => {
    setEdits(new Map())
    past.current = []
    future.current = []
    setExtraSubjects([])
    setCellErrors(new Map())
    setSaveError(null)
  }

  const selectedInfo = selected ? describeCell(selected, v, amountRows) : null
  const otherScenarios = scenarios.filter((s) => s.id !== v.scenario.id)

  return (
    <>
      <div className="mb-2 flex flex-wrap gap-4 text-sm">
        <Link to={`/scenarios/${v.scenario.id}`} className="text-slate-500 hover:text-slate-700">
          ← {v.scenario.name}
        </Link>
        <Link to={`/activities/${v.activity.id}`} className="text-slate-500 hover:text-slate-700">
          施策の詳細 →
        </Link>
        <Link to={`/history?activity_id=${v.activity.id}&scenario_id=${v.scenario.id}`} className="text-slate-500 hover:text-slate-700">
          このシナリオでの変更履歴 →
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
      {active && active.id !== v.scenario.id && (
        <p className="mb-4 rounded-md border border-emerald-100 bg-emerald-50/70 px-4 py-2 text-sm text-emerald-900">
          このシナリオは作成中ではありません。
          <Link to={`/scenarios/${active.id}/activities/${v.activity.id}`} className="ml-1 font-medium underline">
            作成中のシナリオ「{active.name}」で開く →
          </Link>
        </p>
      )}
      {editable && (
        <details className="mb-4 text-xs text-slate-500">
          <summary className="cursor-pointer select-none">スプレッドシートと同じように入力できます（操作方法）</summary>
          <ul className="mt-2 grid gap-x-6 gap-y-1 sm:grid-cols-2">
            <li>セルを選んでそのまま入力。Enter・F2・ダブルクリックで編集</li>
            <li>Enter で確定して下へ、Tab で確定して右へ。Esc で取り消し</li>
            <li>矢印キーで移動。Shift+クリック・Shift+矢印・ドラッグで範囲を選択</li>
            <li>Excel・Google スプレッドシートとコピー・貼り付けできます（1,000 や △1,000 も可）</li>
            <li>Ctrl/⌘+R で右へ、Ctrl/⌘+D で下へコピー（例: 4月の値を3月まで）</li>
            <li>Delete で消去。Ctrl/⌘+Z で元に戻す、Ctrl/⌘+Shift+Z でやり直す</li>
          </ul>
        </details>
      )}

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
          <Grid sheet={driverSheet} label="ドライバー・KPI" months={v.months} actualMonths={actualMonths}>
            {v.drivers.map((d, r) => (
              <tr key={d.id}>
                <RowHeader title={d.name} sub={`${d.code}${d.unit ? `（${d.unit}）` : ''}`} />
                {v.months.map((m, c) => {
                  const k: CellKey = `d:${d.id}:${m}`
                  return (
                    <SheetCell
                      key={m}
                      sheet={driverSheet}
                      r={r}
                      c={c}
                      label={`${d.name} ${monthLabel(m)}`}
                      display={formatNumber(cell(k).value)}
                      editable={editable && !actualMonths.has(m)}
                      provisional={cell(k).is_provisional}
                      changed={isChanged(k)}
                      error={cellErrors.get(k)}
                    />
                  )
                })}
                <td className="border-b border-slate-100" />
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
        <p className="mb-3 text-xs text-slate-500">
          科目の金額は、内訳と「その他」（科目への直接入力）の合計です。内訳は
          <Link to={`/activities/${v.activity.id}`} className="text-indigo-700 underline">
            施策の詳細
          </Link>
          で追加できます。
          {hasFormulaLines && (
            <>
              {' '}
              <span className="rounded bg-indigo-50 px-1 font-mono text-indigo-700">fx</span> の内訳は計算式で算出します。ドライバー値を保存すると再計算されます。
            </>
          )}
        </p>
        {amountRows.length === 0 ? (
          <p className="text-sm text-slate-500">金額はまだありません。{editable ? '「＋ 科目を追加」から入力する科目を選んでください。' : ''}</p>
        ) : (
          <Grid sheet={amountSheet} label="金額" months={v.months} actualMonths={actualMonths} total>
            {amountRows.flatMap((r) => {
              const lines = sheetLines.map((l, i) => [l, i] as const).filter(([l]) => l.row === r)
              if (r.lines.length === 0) return lines.map(([l, i]) => amountLine(l, i))
              return [
                <tr key={r.subject_id} className="bg-slate-50/60">
                  <RowHeader title={r.name} sub={`${r.code} ${categoryLabels[r.category]}・合計`} />
                  {v.months.map((m) => (
                    <td key={m} className="border-b border-slate-100 px-2 py-1 text-right font-medium tabular-nums">
                      {formatYen(String(subjectMonth(r, m)))}
                    </td>
                  ))}
                  <td className="border-b border-slate-100 bg-slate-50 px-2 text-right font-medium tabular-nums">{formatYen(String(subjectTotal(r)))}</td>
                </tr>,
                ...lines.map(([l, i]) => amountLine(l, i)),
              ]
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
            {selectedInfo.editable && editable && !actualMonths.has(selectedInfo.month) ? (
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

      <ConditionCard path={path} condition={v.condition} editable={editable} onSaved={setData} />

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
  if (!v.scenario.is_active) return 'このシナリオは作成中ではないため、参照のみです（入力できるのは FP&A のみ）。'
  return 'この施策の編集権限がないため、参照のみです。'
}

function describeCell(k: CellKey, v: ValuesView, amountRows: AmountRow[]) {
  const parts = k.split(':')
  const month = parts[parts.length - 1]
  if (parts[0] === 'd') {
    return { label: v.drivers.find((d) => d.id === Number(parts[1]))?.name ?? '', month, editable: true }
  }
  const row = amountRows.find((r) => r.subject_id === Number(parts[1]))
  const lineId = Number(parts[2])
  if (!row || lineId === 0) return { label: row ? (row.lines.length > 0 ? `${row.name} その他` : row.name) : '', month, editable: true }
  const line = row.lines.find((l) => l.id === lineId)
  return { label: `${row.name} ${line?.name ?? ''}`, month, editable: !line?.formula_enabled }
}

/** 入力中の金額を整数として足し合わせる（数値でない入力は 0 として扱う） */
function toBigInt(raw: string): bigint {
  const v = normalize(raw)
  return /^-?\d+$/.test(v) ? BigInt(v) : 0n
}

function Grid({
  sheet,
  label,
  months,
  actualMonths,
  total,
  children,
}: {
  sheet: Sheet
  label: string
  months: string[]
  actualMonths: Set<string>
  total?: boolean
  children: ReactNode
}) {
  return (
    <SheetFrame sheet={sheet} label={label}>
      <thead>
        <tr>
          <th className="sticky left-0 z-10 min-w-44 border-b border-slate-200 bg-white px-3 py-1.5 text-left text-xs font-semibold text-slate-500" />
          {months.map((m) => (
            <th key={m} className="border-b border-slate-200 px-2 py-1.5 text-right text-xs font-semibold whitespace-nowrap text-slate-500">
              {monthLabel(m)}
              {actualMonths.has(m) && <span className="block text-[10px] font-normal text-slate-400">実績</span>}
            </th>
          ))}
          <th className="border-b border-slate-200 bg-slate-50 px-2 py-1.5 text-right text-xs font-semibold text-slate-500">{total ? '年計' : ''}</th>
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </SheetFrame>
  )
}

function RowHeader({ title, sub, indent }: { title: ReactNode; sub: string; indent?: boolean }) {
  return (
    <th scope="row" className={cx('sticky left-0 z-10 border-b border-slate-100 bg-white py-1 pr-3 text-left font-normal', indent ? 'pl-7' : 'pl-3')}>
      <div className="text-sm font-medium whitespace-nowrap text-slate-800">{title}</div>
      <div className="font-mono text-xs whitespace-nowrap text-slate-400">{sub}</div>
    </th>
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
      <p className="mb-2 text-xs text-slate-500">このシナリオで想定している内容（楽観・悲観の見通しなど）や、その発生条件を記録します。</p>
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
