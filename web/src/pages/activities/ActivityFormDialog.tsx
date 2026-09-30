import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import {
  activityStatusLabels,
  activityTypeLabels,
  calcModeLabels,
  type Activity,
  type ActivityStatus,
  type ActivityType,
  type CalcMode,
  type Unit,
  type User,
} from '../../api/types'
import { Button, Dialog, Field, FormError, Input, Select, Textarea, fieldError } from '../../components/ui'

const fields = ['unit_id', 'code', 'name', 'activity_type', 'status', 'start_date', 'end_date', 'owner_user_id', 'calc_mode', 'probability', 'assumptions']

/** 確度（0〜1）→ パーセントの入力値 */
function toPercent(p: number | null): string {
  return p === null ? '' : String(Math.round(p * 10000) / 100)
}

/** パーセントの入力値 → 確度（0〜1、小数4桁の文字列）。空なら null */
function fromPercent(s: string): string | null {
  if (s.trim() === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? (n / 100).toFixed(4) : s
}

/**
 * 施策の作成・編集ダイアログ。
 * 保存は save(body) に任せる（編集時は変更理由の入力を挟むため、呼び出し側で withReason を使う）。
 */
export function ActivityFormDialog({
  initial,
  units,
  users,
  onClose,
  save,
}: {
  initial: Activity | null
  units: Unit[]
  users: User[]
  onClose: () => void
  save: (body: Record<string, unknown>) => Promise<void>
}) {
  const [unitId, setUnitId] = useState(initial ? String(initial.unit_id) : units.length === 1 ? String(units[0].id) : '')
  const [code, setCode] = useState(initial?.code ?? '')
  const [name, setName] = useState(initial?.name ?? '')
  const [activityType, setActivityType] = useState<ActivityType>(initial?.activity_type ?? 'recurring')
  const [status, setStatus] = useState<ActivityStatus>(initial?.status ?? 'planned')
  const [startDate, setStartDate] = useState(initial?.start_date ?? '')
  const [endDate, setEndDate] = useState(initial?.end_date ?? '')
  const [ownerId, setOwnerId] = useState(initial?.owner_user_id ? String(initial.owner_user_id) : '')
  const [calcMode, setCalcMode] = useState<CalcMode>(initial?.calc_mode ?? 'manual')
  const [probability, setProbability] = useState(toPercent(initial?.probability ?? null))
  const [assumptions, setAssumptions] = useState(initial?.assumptions ?? '')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await save({
        unit_id: Number(unitId) || 0,
        code,
        name,
        activity_type: activityType,
        status,
        start_date: startDate || null,
        end_date: endDate || null,
        owner_user_id: ownerId ? Number(ownerId) : null,
        calc_mode: calcMode,
        probability: fromPercent(probability),
        assumptions,
      })
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      wide
      title={initial ? '施策の編集' : '施策の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="activity-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="activity-form" onSubmit={submit} className="grid grid-cols-2 gap-4">
        <Field label="施策名" required error={fieldError(error, 'name')} className="col-span-2">
          {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <Field
          label="施策コード"
          required={initial !== null}
          error={fieldError(error, 'code')}
          hint={initial ? 'アプリ内で施策を指す番号（英数字・-・_）' : '空欄なら自動で採番します（ACT-0001 形式）'}
        >
          {(p) => <Input {...p} value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" placeholder={initial ? undefined : '自動採番'} />}
        </Field>
        <Field label="ユニット" required error={fieldError(error, 'unit_id')}>
          {(p) => (
            <Select {...p} value={unitId} onChange={(e) => setUnitId(e.target.value)}>
              <option value="">選択してください</option>
              {units.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="施策タイプ" required error={fieldError(error, 'activity_type')}>
          {(p) => (
            <Select {...p} value={activityType} onChange={(e) => setActivityType(e.target.value as ActivityType)}>
              {Object.entries(activityTypeLabels).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="ステータス" required error={fieldError(error, 'status')}>
          {(p) => (
            <Select {...p} value={status} onChange={(e) => setStatus(e.target.value as ActivityStatus)}>
              {Object.entries(activityStatusLabels).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="開始日" required={activityType === 'project'} error={fieldError(error, 'start_date')}>
          {(p) => <Input {...p} type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />}
        </Field>
        <Field label="終了日" required={activityType === 'project'} error={fieldError(error, 'end_date')} hint={activityType === 'recurring' ? '運用型は空欄にできます' : undefined}>
          {(p) => <Input {...p} type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />}
        </Field>
        <Field label="担当者" error={fieldError(error, 'owner_user_id')} hint="担当者はこの施策を編集できます">
          {(p) => (
            <Select {...p} value={ownerId} onChange={(e) => setOwnerId(e.target.value)}>
              <option value="">（未設定）</option>
              {users
                .filter((u) => u.is_active || String(u.id) === ownerId)
                .map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="確度（%）" error={fieldError(error, 'probability')} hint="案件や売上の発生確度。計算式では probability で参照できます">
          {(p) => <Input {...p} type="number" min={0} max={100} step="0.01" value={probability} onChange={(e) => setProbability(e.target.value)} />}
        </Field>
        <Field label="金額の算出方式" required error={fieldError(error, 'calc_mode')} className="col-span-2">
          {(p) => (
            <Select {...p} value={calcMode} onChange={(e) => setCalcMode(e.target.value as CalcMode)}>
              <option value="manual">{calcModeLabels.manual}（金額を直接入力し、ドライバーは根拠として表示）</option>
              <option value="formula">{calcModeLabels.formula}（ドライバーの値と計算式から金額を算出）</option>
            </Select>
          )}
        </Field>
        <Field label="前提条件" error={fieldError(error, 'assumptions')} className="col-span-2">
          {(p) => <Textarea {...p} value={assumptions} onChange={(e) => setAssumptions(e.target.value)} placeholder="例: A社の年間契約更新が前提。単価は2026年度の改定後価格" />}
        </Field>
        <div className="col-span-2">
          <FormError error={error} fields={fields} />
        </div>
      </form>
    </Dialog>
  )
}

/** 施策を作成する（作成は変更理由不要） */
export async function createActivity(body: Record<string, unknown>): Promise<Activity> {
  return api.post<Activity>('/activities', body)
}
