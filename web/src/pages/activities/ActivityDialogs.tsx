import { useRef, useState, type FormEvent } from 'react'
import { categoryLabels, driverKindLabels, milestoneStatusLabels, type Driver, type DriverKind, type Formula, type Milestone, type MilestoneStatus, type Subject } from '../../api/types'
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

export function FormulaDialog({
  initial,
  subjects,
  drivers,
  onClose,
  save,
}: {
  initial: Formula | null
  subjects: Subject[]
  drivers: Driver[]
  onClose: () => void
  save: (subjectId: number, body: Record<string, unknown>) => Promise<void>
}) {
  const [subjectId, setSubjectId] = useState(initial ? String(initial.subject_id) : '')
  const [expression, setExpression] = useState(initial?.expression ?? '')
  const [reason, setReason] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const { error, busy, submit } = useSubmit(async () => {
    if (!subjectId) throw new Error('科目を選択してください')
    await save(Number(subjectId), { expression, reason })
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

  return (
    <Dialog open wide title={initial ? '計算式の編集' : '計算式の追加'} onClose={onClose} footer={<Footer form="formula-form" busy={busy} onClose={onClose} />}>
      <form id="formula-form" onSubmit={submit} className="space-y-4">
        <Field label="科目" required>
          {(p) => (
            <Select {...p} value={subjectId} disabled={initial !== null} onChange={(e) => setSubjectId(e.target.value)}>
              <option value="">選択してください</option>
              {subjects.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.code} {s.name}（{categoryLabels[s.category]}）
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="計算式" required error={fieldError(error, 'expression')} hint="四則演算（+ - * /）と括弧が使えます。結果は円未満を四捨五入します。費用はマイナスで計上する場合、式の先頭に - を付けます">
          {(p) => <Input {...p} ref={inputRef} value={expression} onChange={(e) => setExpression(e.target.value)} className="font-mono" placeholder="unit_price * volume * probability" />}
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
            <button type="button" onClick={() => insert('probability')} className="rounded border border-slate-200 bg-slate-50 px-2 py-0.5 font-mono text-xs text-slate-700 hover:bg-indigo-50">
              probability<span className="ml-1 font-sans text-slate-400">施策の確度</span>
            </button>
          </div>
          {drivers.length === 0 && <p className="mt-1 text-xs text-amber-700">ドライバーが未登録です。先にドライバーを追加してください。</p>}
        </div>
        <Field label="変更理由" required error={fieldError(error, 'reason')} hint="計算式の変更は金額に影響するため、理由を記録します">
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['expression', 'reason']} />
      </form>
    </Dialog>
  )
}
