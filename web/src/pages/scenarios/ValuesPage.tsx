import { useEffect, useMemo, useRef, useState, type DragEvent, type ReactNode } from 'react'
import { api, ApiError } from '../../api/client'
import { categoryLabels, outlookLabels, type AmountRow, type List, type Scenario, type Subject, type ValuesView } from '../../api/types'
import { SheetCell, SheetFrame, useSheet, type Sheet } from '../../components/Sheet'
import { Badge, Button, Card, ErrorMessage, Loading, Select, Textarea, cx } from '../../components/ui'
import { formatNumber, formatYen, monthLabel, yearMonthLabel } from '../../lib/format'
import { useActiveScenario } from '../../lib/activeScenario'
import { useCurrentUser } from '../../lib/auth'
import { Link } from '../../lib/router'
import { defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { inputSubjects } from '../../lib/visibility'
import { RestrictedNote } from '../../components/RestrictedNote'
import { Help } from '../../components/Help'
import { ComparisonCard } from './ComparisonCard'
import { NoteCard } from './NoteCard'

/** 1セルの入力内容。value が '' なら削除 */
type CellEdit = { value: string; is_provisional: boolean; provisional_reason: string }

/** セルのキー。d = ドライバー値（d:ドライバー:月）、a = 金額（a:科目:内訳:月。内訳 0 は科目への直接入力） */
type CellKey = `d:${number}:${string}` | `a:${number}:${number}:${string}`

type ServerCell = CellEdit & { source?: string }

const sourceLabels: Record<string, string> = { manual: '直接入力', formula: '計算式', import: '取込', actual: '実績' }

function normalize(v: string): string {
  return v.replace(/,/g, '').trim()
}

/** 施策の画面の内訳・ドライバーの追加（「今回の更新」の表から開く。docs/plan.md「2.19」） */
export type PanelActions = { addLine: () => void; addDriver: () => void }

/**
 * シナリオ×施策の月別の数値を表示・入力するパネル（施策の画面の「今回の更新」タブ。docs/plan.md「2.19」）。
 * refreshKey が変わると数値を読み直す（内訳・ドライバーを追加・変更したとき）。未保存の入力の有無は onDirtyChange で知らせる。
 */
export function ValuesPanel({
  scenarioId,
  activityId,
  refreshKey,
  onDirtyChange,
  actions,
}: {
  scenarioId: number
  activityId: number
  refreshKey: number
  onDirtyChange: (dirty: boolean) => void
  /** 施策を編集できるときだけ渡す */
  actions?: PanelActions
}) {
  const path = `/scenarios/${scenarioId}/activities/${activityId}`
  const view = useApi<ValuesView>(path)
  const subjects = useApi<List<Subject>>('/subjects')
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const { reload } = view
  const loadedKey = useRef(refreshKey)
  useEffect(() => {
    if (refreshKey === loadedKey.current) return
    loadedKey.current = refreshKey
    reload()
  }, [refreshKey, reload])

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
      onDirtyChange={onDirtyChange}
      actions={actions}
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
  onDirtyChange,
  actions,
}: {
  path: string
  v: ValuesView
  subjects: Subject[]
  scenarios: Scenario[]
  setData: (v: ValuesView) => void
  reload: () => Promise<void>
  onDirtyChange: (dirty: boolean) => void
  actions?: PanelActions
}) {
  const { active } = useActiveScenario()
  const me = useCurrentUser()

  // 比較するシナリオ: 基準（修正計画、なければ期初計画）と前回見込（シナリオに FP&A が指定）
  const baseScenario = defaultScenarios(scenarios.filter((s) => s.fiscal_year === v.scenario.fiscal_year)).base
  const previousScenario = scenarios.find((s) => s.id === v.scenario.previous_scenario_id)
  const baseView = useApi<ValuesView>(baseScenario ? `/scenarios/${baseScenario.id}/activities/${v.activity.id}` : null)
  const previousView = useApi<ValuesView>(previousScenario ? `/scenarios/${previousScenario.id}/activities/${v.activity.id}` : null)
  const [edits, setEdits] = useState<Map<CellKey, CellEdit>>(new Map())
  const [extraSubjects, setExtraSubjects] = useState<number[]>([])
  const [selected, setSelected] = useState<CellKey | null>(null)
  const [reason, setReason] = useState('')
  const [saveError, setSaveError] = useState<unknown>(null)
  const [cellErrors, setCellErrors] = useState<Map<CellKey, string>>(new Map())
  const [saving, setSaving] = useState(false)
  const [savedMessage, setSavedMessage] = useState('')
  const [driverOrder, setDriverOrder] = useState<number[] | null>(null)
  const [orderError, setOrderError] = useState<unknown>(null)
  const [dragFrom, setDragFrom] = useState<number | null>(null)
  const [dragOver, setDragOver] = useState<number | null>(null)

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
  const cell = (k: CellKey): CellEdit => {
    const e = edits.get(k)
    if (e) return e
    if (activePreview?.cells && formulaKeys.has(k)) return activePreview.cells.get(k) ?? { value: '', is_provisional: false, provisional_reason: '' }
    return serverCell(k)
  }
  /** 試算で、保存済みの金額から変わるセル */
  const isPreviewed = (k: CellKey) => !!activePreview?.cells && formulaKeys.has(k) && normalize(cell(k).value) !== serverCell(k).value
  const isChanged = (k: CellKey) => {
    const e = edits.get(k)
    if (!e) return false
    const s = serverCell(k)
    return normalize(e.value) !== s.value || e.is_provisional !== s.is_provisional || (e.is_provisional && e.provisional_reason !== s.provisional_reason)
  }
  const changedKeys = [...edits.keys()].filter(isChanged)
  const dirty = changedKeys.length > 0

  // --- 保存前の試算（ドライバー値を変えたら、計算式の内訳の金額をサーバーで試算する） ---
  const driverItems = changedKeys
    .filter((k) => k.startsWith('d:'))
    .map((k) => {
      const [, id, month] = k.split(':')
      const e = edits.get(k)!
      const value = normalize(e.value)
      return { driver_id: Number(id), target_month: month, value: value === '' ? null : value, is_provisional: e.is_provisional, provisional_reason: e.is_provisional ? e.provisional_reason : '' }
    })
  const driverPayload = JSON.stringify(driverItems)
  const [preview, setPreview] = useState<{ payload: string; cells?: Map<CellKey, ServerCell>; error?: unknown } | null>(null)
  useEffect(() => {
    if (driverPayload === '[]' || !v.editable) return
    let cancelled = false
    const timer = setTimeout(async () => {
      try {
        const res = await api.put<ValuesView>(`${path}/driver-values?dry_run=true`, { values: JSON.parse(driverPayload) })
        if (!cancelled) setPreview({ payload: driverPayload, cells: formulaCells(res) })
      } catch (err) {
        if (!cancelled) setPreview({ payload: driverPayload, error: err })
      }
    }, 400)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [driverPayload, path, v.editable])
  // 今の入力に対する試算だけを使う（入力が変わったら、次の試算が届くまで「試算中」）
  const activePreview = driverItems.length > 0 && preview?.payload === driverPayload ? preview : null
  const previewing = driverItems.length > 0 && v.editable && !activePreview
  const formulaKeys = new Set<CellKey>(v.amounts.flatMap((a) => a.lines.filter((l) => l.formula_enabled).flatMap((l) => v.months.map((m) => `a:${a.subject_id}:${l.id}:${m}` as CellKey))))

  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange])

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
  const addableSubjects = inputSubjects(subjects, me.role).filter((s) => !amountRows.some((r) => r.subject_id === s.id))
  const hasFormulaLines = amountRows.some((r) => r.lines.some((l) => l.formula_enabled))

  /** 科目の月の金額（内訳と科目への直接入力の合計） */
  const subjectMonth = (r: AmountRow, month: string) => [0, ...r.lines.map((l) => l.id)].reduce((sum, lineId) => sum + toBigInt(cell(`a:${r.subject_id}:${lineId}:${month}`).value), 0n)
  const monthTotal = (category: 'revenue' | 'expense', month: string) => amountRows.filter((r) => r.category === category).reduce((sum, r) => sum + subjectMonth(r, month), 0n)
  /** 比較の「今回」: 画面で入力中の値（未保存の入力と保存前の試算を含む）を、数値入力 API の amounts と同じ形にする */
  const currentRows: AmountRow[] = amountRows.map((r) => {
    const valuesOf = (lineId: number) =>
      v.months.flatMap((m) => {
        const raw = normalize(cell(`a:${r.subject_id}:${lineId}:${m}`).value)
        return /^-?\d+$/.test(raw) ? [{ target_month: m, amount: raw, source: 'manual' as const, is_provisional: false, provisional_reason: '' }] : []
      })
    return { ...r, values: valuesOf(0), lines: r.lines.map((l) => ({ ...l, values: valuesOf(l.id) })) }
  })
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
        sub: `${l.formula_enabled ? l.expression : '直接入力'} ／ ${outlookLabels[l.outlook]}・${l.confidence_level ?? v.activity.confidence_level}`,
        editable: !l.formula_enabled,
        indent: true,
      })),
      { row: r, lineId: 0, label: `${r.name} その他`, title: 'その他', sub: '科目への直接入力', editable: true, indent: true },
    ]
  })
  // ドライバーの並び（ドラッグで並び替えた直後は、保存が終わるまでその順で表示する）
  const drivers = driverOrder ? driverOrder.map((id) => v.drivers.find((d) => d.id === id)!).filter(Boolean) : v.drivers
  const canReorder = v.activity.can_edit && v.drivers.length > 1
  const moveDriver = async (from: number, to: number) => {
    if (from === to || to < 0 || to >= drivers.length) return
    const ids = drivers.map((d) => d.id)
    const [moved] = ids.splice(from, 1)
    ids.splice(to, 0, moved)
    setDriverOrder(ids)
    setOrderError(null)
    try {
      await api.put(`/activities/${v.activity.id}/drivers/order`, { ids })
      await reload()
    } catch (err) {
      setOrderError(err)
    } finally {
      setDriverOrder(null)
    }
  }

  const readonlyKeys = new Set<CellKey>(sheetLines.filter((l) => !l.editable).flatMap((l) => v.months.map((m) => `a:${l.row.subject_id}:${l.lineId}:${m}` as CellKey)))
  const sheetOptions = {
    isEditable: (k: CellKey) => editable && !readonlyKeys.has(k) && !actualMonths.has(monthOf(k)),
    rawValue: (k: CellKey) => normalize(cell(k).value),
    setValues,
    onActivate: (k: CellKey) => setSelected(k),
    onUndo: undo,
    onRedo: redo,
  }
  const driverSheet = useSheet<CellKey>({ ...sheetOptions, grid: drivers.map((d) => v.months.map((m) => `d:${d.id}:${m}` as CellKey)) })
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
            changed={isChanged(k) || isPreviewed(k)}
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

  return (
    <>
      {!editable && <p className="mb-4 rounded-md bg-slate-100 px-4 py-2 text-sm text-slate-700">{readonlyReason(v)}</p>}
      {active && active.id !== v.scenario.id && (
        <p className="mb-4 rounded-md border border-emerald-100 bg-emerald-50/70 px-4 py-2 text-sm text-emerald-900">
          このシナリオは今回の見込ではありません。
          <Link to={`/activities/${v.activity.id}?tab=update&scenario=${active.id}`} className="ml-1 font-medium underline">
            今回の見込「{active.name}」で開く →
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

      <Card
        title="ドライバー・KPI"
        className="mb-4"
        actions={
          actions && (
            <Button size="sm" variant="ghost" onClick={actions.addDriver}>
              ＋ ドライバーを追加
            </Button>
          )
        }
      >
        {v.drivers.length === 0 ? (
          <p className="text-sm text-slate-500">ドライバーがありません。単価・件数・顧客数など、金額の根拠となる値を追加できます。</p>
        ) : (
          <Grid sheet={driverSheet} label="ドライバー・KPI" months={v.months} actualMonths={actualMonths}>
            {drivers.map((d, r) => (
              <tr
                key={d.id}
                onDragOver={(e) => {
                  if (dragFrom === null) return
                  e.preventDefault()
                  setDragOver(r)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  if (dragFrom !== null) moveDriver(dragFrom, r)
                  setDragFrom(null)
                  setDragOver(null)
                }}
                className={cx(dragOver === r && dragFrom !== null && dragFrom !== r && (dragFrom < r ? 'shadow-[inset_0_-2px_0_0_var(--color-indigo-500)]' : 'shadow-[inset_0_2px_0_0_var(--color-indigo-500)]'))}
              >
                <RowHeader
                  title={d.name}
                  sub={`${d.code}${d.unit ? `（${d.unit}）` : ''}`}
                  handle={
                    canReorder && (
                      <DragHandle
                        label={`${d.name}を並び替え`}
                        onDragStart={(e) => {
                          setDragFrom(r)
                          const row = (e.currentTarget as HTMLElement).closest('tr')
                          if (row) e.dataTransfer.setDragImage(row, 16, 16)
                          e.dataTransfer.effectAllowed = 'move'
                        }}
                        onDragEnd={() => {
                          setDragFrom(null)
                          setDragOver(null)
                        }}
                        onMove={(delta) => moveDriver(r, r + delta)}
                      />
                    )
                  }
                />
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
        {canReorder && <p className="mt-2 text-xs text-slate-400">行の左端の ⋮⋮ をドラッグすると並び替えられます（選んで ↑↓ キーでも移動できます）。</p>}
        {orderError ? (
          <div className="mt-2">
            <ErrorMessage error={orderError} />
          </div>
        ) : null}
      </Card>

      <RestrictedNote hidden={v.restricted_hidden} className="mb-2" />
      <Card
        title={
          <>
            金額（円）
            <Help manual="member#amounts">
              科目の金額は、内訳と「その他」（科目への直接入力）の合計です。内訳は「＋ 内訳を追加」で追加できます（計算式などの詳しい設定は「設定」タブ）。
              {hasFormulaLines && <> fx の内訳は計算式で算出します。ドライバー値を変えると、保存前に試算した金額を表示します。</>}
            </Help>
          </>
        }
        className="mb-4"
        actions={
          <span className="flex items-center gap-2">
            {actions && (
              <Button size="sm" variant="ghost" onClick={actions.addLine}>
                ＋ 内訳を追加
              </Button>
            )}
            {editable && addableSubjects.length > 0 && (
              <Select aria-label="科目を追加" value="" onChange={(e) => e.target.value && setExtraSubjects((p) => [...p, Number(e.target.value)])} className="w-48 py-1 text-xs">
                <option value="">＋ 科目を追加…</option>
                {addableSubjects.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.code} {s.name}
                  </option>
                ))}
              </Select>
            )}
          </span>
        }
      >
        {hasFormulaLines && driverItems.length > 0 && (
          <p className="mb-3 rounded bg-indigo-50 px-3 py-1.5 text-xs text-indigo-800" role="status" aria-label="試算の状態">
            {previewing
              ? '試算中…'
              : activePreview?.error
                ? `試算できません: ${previewErrorMessage(activePreview.error, driverItems, v)}`
                : '色の付いた fx のセルは、保存前の試算です（まだ保存していません）。'}
          </p>
        )}
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

      <ComparisonCard
        months={v.months}
        current={currentRows}
        compares={[
          {
            label: '目標',
            rows: baseScenario ? baseView.data?.amounts : undefined,
            note: baseScenario ? `${scenarioLabel(baseScenario)}${baseScenario.id === v.scenario.id ? '（このシナリオ）' : ''}` : '未設定（期初計画・修正計画のエイリアスがありません）',
          },
          {
            label: '前回の見込',
            rows: previousScenario ? previousView.data?.amounts : undefined,
            note: previousScenario ? scenarioLabel(previousScenario) : '未設定（シナリオ管理で指定します）',
          },
        ]}
      />

      <NoteCard
        path={path}
        view={v}
        editable={editable}
        dirty={dirty}
        months={v.months}
        current={currentRows}
        base={baseScenario ? baseView.data?.amounts : undefined}
        previous={previousScenario ? previousView.data?.amounts : undefined}
        onSaved={setData}
      />

      {selected && selectedInfo && (
        <Card title={`選択中のセル: ${selectedInfo.label}（${yearMonthLabel(selectedInfo.month)}）`} className="mb-4">
          <div className="flex flex-wrap items-start gap-6 text-sm">
            {selected.startsWith('a:') && server.get(selected)?.source && <div>登録方法: {sourceLabels[server.get(selected)!.source!]}</div>}
            {selectedInfo.editable && editable && !actualMonths.has(selectedInfo.month) ? (
              // 仮の値は「詳細設定」（docs/plan.md「2.18」）。仮の値のセルでは開いておく
              <details key={selected} open={cell(selected).is_provisional} className="flex-1">
                <summary className="cursor-pointer text-xs text-slate-500 select-none">詳細設定（仮の値）</summary>
                <div className="mt-2 flex flex-wrap items-start gap-6">
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
                </div>
              </details>
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
                  placeholder={v.scenario.is_active ? `変更理由（任意。空欄なら「${v.scenario.name}の見込更新」と記録します）` : '変更理由（必須）例: 10月の受注確定を反映'}
                  className={cx('min-h-10 flex-1', saveError instanceof ApiError && saveError.details.reason ? 'border-red-400' : '')}
                  rows={1}
                />
                <Button onClick={discard} disabled={saving}>
                  取り消し
                </Button>
                <Button variant="primary" onClick={save} disabled={saving || (reason.trim() === '' && !v.scenario.is_active)}>
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
  if (!v.scenario.is_active) return 'このシナリオは今回の見込ではないため、参照のみです（入力できるのは FP&A のみ）。'
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

function RowHeader({ title, sub, indent, handle }: { title: ReactNode; sub: string; indent?: boolean; handle?: ReactNode }) {
  return (
    <th scope="row" className={cx('sticky left-0 z-10 border-b border-slate-100 bg-white py-1 pr-3 text-left font-normal', handle ? 'pl-1' : indent ? 'pl-7' : 'pl-3')}>
      <div className="flex items-center gap-1">
        {handle}
        <div>
          <div className="text-sm font-medium whitespace-nowrap text-slate-800">{title}</div>
          <div className="font-mono text-xs whitespace-nowrap text-slate-400">{sub}</div>
        </div>
      </div>
    </th>
  )
}

/** 行を並び替えるつまみ。ドラッグするか、フォーカスして ↑↓ キーで動かす */
function DragHandle({
  label,
  onDragStart,
  onDragEnd,
  onMove,
}: {
  label: string
  onDragStart: (e: DragEvent<HTMLButtonElement>) => void
  onDragEnd: () => void
  onMove: (delta: number) => void
}) {
  return (
    <button
      type="button"
      draggable
      aria-label={`${label}（ドラッグ、または ↑↓ キー）`}
      title="ドラッグで並び替え"
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onMouseDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        // グリッドの矢印キー操作に渡さない
        if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
          e.preventDefault()
          e.stopPropagation()
          onMove(e.key === 'ArrowUp' ? -1 : 1)
        }
      }}
      className="cursor-grab rounded px-1 py-1 text-slate-300 hover:bg-slate-100 hover:text-slate-600 active:cursor-grabbing"
    >
      ⋮⋮
    </button>
  )
}

/**
 * 試算のエラーを、どのセルの何が問題かが分かる文にする（例: 「月額単価 5月: 小数点以下は6桁までで入力してください」）。
 * 422 の見出し（入力内容に誤りがあります）だけでは理由が分からないため、項目ごとのメッセージを使う。
 */
function previewErrorMessage(err: unknown, items: { driver_id: number; target_month: string }[], v: ValuesView): string {
  if (!(err instanceof ApiError)) return '通信に失敗しました'
  const messages = Object.entries(err.details).map(([field, msg]) => {
    const m = field.match(/^values\[(\d+)\]/)
    const item = m ? items[Number(m[1])] : undefined
    const driver = item && v.drivers.find((d) => d.id === item.driver_id)
    return driver ? `${driver.name} ${monthLabel(item.target_month)}: ${msg}` : msg
  })
  return messages.length > 0 ? [...new Set(messages)].slice(0, 3).join(' ／ ') : err.message
}

/** 試算の結果から、計算式で反映する内訳のセルを取り出す */
function formulaCells(view: ValuesView): Map<CellKey, ServerCell> {
  const m = new Map<CellKey, ServerCell>()
  for (const a of view.amounts) {
    for (const l of a.lines) {
      if (!l.formula_enabled) continue
      for (const c of l.values) {
        m.set(`a:${a.subject_id}:${l.id}:${c.target_month}`, { value: String(c.amount), is_provisional: c.is_provisional, provisional_reason: c.provisional_reason, source: c.source })
      }
    }
  }
  return m
}
