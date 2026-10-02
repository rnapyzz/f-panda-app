import { useRef, useState, type FormEvent } from 'react'
import { categoryLabels, driverKindLabels, milestoneStatusLabels, type Driver, type DriverKind, type Line, type Milestone, type MilestoneStatus, type Subject } from '../../api/types'
import { Button, Dialog, Field, FormError, Input, Select, Textarea, fieldError } from '../../components/ui'

/** ダイアログの保存処理と状態をまとめる */
function useSubmit(save: () => Promise<void>) {
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await save()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  return { error, busy, submit }
}

function Footer({ form, busy, onClose }: { form: string; busy: boolean; onClose: () => void }) {
  return (
    <>
      <Button onClick={onClose}>キャンセル</Button>
      <Button variant="primary" type="submit" form={form} disabled={busy}>
        {busy ? '保存中…' : '保存'}
      </Button>
    </>
  )
}

export function MilestoneDialog({ initial, onClose, save }: { initial: Milestone | null; onClose: () => void; save: (body: Record<string, unknown>) => Promise<void> }) {
  const [name, setName] = useState(initial?.name ?? '')
  const [dueDate, setDueDate] = useState(initial?.due_date ?? '')
  const [status, setStatus] = useState<MilestoneStatus>(initial?.status ?? 'not_started')
  const { error, busy, submit } = useSubmit(() => save({ name, due_date: dueDate, status }))

  return (
    <Dialog open title={initial ? 'マイルストーンの編集' : 'マイルストーンの追加'} onClose={onClose} footer={<Footer form="milestone-form" busy={busy} onClose={onClose} />}>
      <form id="milestone-form" onSubmit={submit} className="space-y-4">
        <Field label="名称" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 要件定義完了" />}
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="期日" required error={fieldError(error, 'due_date')} hint={initial ? '期日を変えると変更理由を求められます' : undefined}>
            {(p) => <Input {...p} type="date" value={dueDate} onChange={(e) => setDueDate(e.target.value)} />}
          </Field>
          <Field label="状態" required error={fieldError(error, 'status')}>
            {(p) => (
              <Select {...p} value={status} onChange={(e) => setStatus(e.target.value as MilestoneStatus)}>
                {Object.entries(milestoneStatusLabels).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
        <FormError error={error} fields={['name', 'due_date', 'status']} />
      </form>
    </Dialog>
  )
}

export function DriverDialog({ initial, onClose, save }: { initial: Driver | null; onClose: () => void; save: (body: Record<string, unknown>) => Promise<void> }) {
  const [name, setName] = useState(initial?.name ?? '')
  const [code, setCode] = useState(initial?.code ?? '')
  const [kind, setKind] = useState<DriverKind>(initial?.driver_kind ?? 'value')
  const [unit, setUnit] = useState(initial?.unit ?? '')
  const { error, busy, submit } = useSubmit(() => save({ name, code, driver_kind: kind, unit }))

  return (
    <Dialog open title={initial ? 'ドライバーの編集' : 'ドライバーの追加'} onClose={onClose} footer={<Footer form="driver-form" busy={busy} onClose={onClose} />}>
      <form id="driver-form" onSubmit={submit} className="space-y-4">
        <Field label="名称" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 月額単価" />}
        </Field>
        <Field label="コード" required error={fieldError(error, 'code')} hint="計算式で使う名前。英小文字で始まる英小文字・数字・_（例: unit_price）">
          {(p) => <Input {...p} value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" />}
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="種別" required error={fieldError(error, 'driver_kind')}>
            {(p) => (
              <Select {...p} value={kind} onChange={(e) => setKind(e.target.value as DriverKind)}>
                {Object.entries(driverKindLabels).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="単位" error={fieldError(error, 'unit')}>
            {(p) => <Input {...p} value={unit} onChange={(e) => setUnit(e.target.value)} placeholder="例: 円、件、人" />}
          </Field>
        </div>
        <FormError error={error} fields={['name', 'code', 'driver_kind', 'unit']} />
      </form>
    </Dialog>
  )
}

export function LineDialog({
  initial,
  subjects,
  drivers,
  onClose,
  save,
}: {
  initial: Line | null
  subjects: Subject[]
  drivers: Driver[]
  onClose: () => void
  save: (body: Record<string, unknown>) => Promise<void>
}) {
  const [subjectId, setSubjectId] = useState(initial ? String(initial.subject_id) : '')
  const [name, setName] = useState(initial?.name ?? '')
  const [formulaEnabled, setFormulaEnabled] = useState(initial?.formula_enabled ?? false)
  const [expression, setExpression] = useState(initial?.expression ?? '')
  const [reason, setReason] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const { error, busy, submit } = useSubmit(async () => {
    await save({ subject_id: Number(subjectId) || 0, name, expression, formula_enabled: formulaEnabled, reason })
  })

  // 識別子をカーソル位置に挿入する
  const insert = (ident: string) => {
    const el = inputRef.current
    const start = el?.selectionStart ?? expression.length
    const end = el?.selectionEnd ?? expression.length
    const before = expression.slice(0, start)
    const sep = before && !/[\s(]$/.test(before) ? ' ' : ''
    setExpression(before + sep + ident + expression.slice(end))
    requestAnimationFrame(() => el?.focus())
  }

  // 計算式・反映の有無を変えると金額に影響するため、変更理由を求める
  const formulaChanged = initial ? initial.expression !== expression.trim() || initial.formula_enabled !== formulaEnabled : formulaEnabled

  return (
    <Dialog open wide title={initial ? '内訳の編集' : '内訳の追加'} onClose={onClose} footer={<Footer form="line-form" busy={busy} onClose={onClose} />}>
      <form id="line-form" onSubmit={submit} className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="科目" required error={fieldError(error, 'subject_id')}>
            {(p) => (
              <Select
                {...p}
                value={subjectId}
                disabled={initial !== null}
                onChange={(e) => {
                  setSubjectId(e.target.value)
                  if (!name) setName(subjects.find((s) => String(s.id) === e.target.value)?.name ?? '')
                }}
              >
                <option value="">選択してください</option>
                {subjects.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.code} {s.name}（{categoryLabels[s.category]}）
                  </option>
                ))}
              </Select>
            )}
          </Field>
          <Field label="内訳名" required error={fieldError(error, 'name')} hint="例: 月額利用料、初期導入費">
            {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />}
          </Field>
        </div>
        <fieldset>
          <legend className="mb-1 text-sm font-medium text-slate-700">金額の入れ方</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {(
              [
                [false, '直接入力', '金額を入力します。計算式は参考として残せます'],
                [true, '計算式で反映', 'ドライバーの値と計算式から金額を算出します'],
              ] as const
            ).map(([value, label, desc]) => (
              <label
                key={label}
                className={`flex cursor-pointer gap-2 rounded-md border px-3 py-2 text-sm ${formulaEnabled === value ? 'border-indigo-400 bg-indigo-50' : 'border-slate-200 hover:bg-slate-50'}`}
              >
                <input type="radio" name="formula_enabled" checked={formulaEnabled === value} onChange={() => setFormulaEnabled(value)} className="mt-0.5" />
                <span>
                  <span className="font-medium text-slate-800">{label}</span>
                  <span className="block text-xs text-slate-500">{desc}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <Field
          label="計算式"
          required={formulaEnabled}
          error={fieldError(error, 'expression')}
          hint="四則演算（+ - * /）と括弧が使えます。結果は円未満を四捨五入します。金額は満額で算出します（確度は集計時に加重します）"
        >
          {(p) => <Input {...p} ref={inputRef} value={expression} onChange={(e) => setExpression(e.target.value)} className="font-mono" placeholder="unit_price * volume" />}
        </Field>
        <div>
          <p className="mb-1 text-xs font-medium text-slate-500">使える名前（クリックで挿入）</p>
          <div className="flex flex-wrap gap-1.5">
            {drivers.map((d) => (
              <button key={d.id} type="button" onClick={() => insert(d.code)} className="rounded border border-slate-200 bg-slate-50 px-2 py-0.5 font-mono text-xs text-slate-700 hover:bg-indigo-50">
                {d.code}
                <span className="ml-1 font-sans text-slate-400">{d.name}</span>
              </button>
            ))}
          </div>
          {drivers.length === 0 && <p className="mt-1 text-xs text-amber-700">ドライバーが未登録です。計算式を使うには、先にドライバーを追加してください。</p>}
        </div>
        {formulaChanged && (
          <Field label="変更理由" required error={fieldError(error, 'reason')} hint="計算式・反映の有無は金額に影響するため、理由を記録します">
            {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
          </Field>
        )}
        {initial && initial.formula_enabled && !formulaEnabled && (
          <p className="rounded bg-slate-50 px-3 py-2 text-xs text-slate-600">反映をやめても、これまでに算出した金額は残ります。以後は直接入力で編集できます。</p>
        )}
        <FormError error={error} fields={['subject_id', 'name', 'expression', 'reason']} />
      </form>
    </Dialog>
  )
}
