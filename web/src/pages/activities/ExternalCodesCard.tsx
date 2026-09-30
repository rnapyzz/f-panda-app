import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { ExternalCode } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Button, Card, FormError, Input, fieldError } from '../../components/ui'

/**
 * 外部コード（会計・基幹システムの案件番号など）。案件化したときに登録し、実績 CSV の取込で施策を特定するのに使う。
 * 枠の施策には複数の外部コードを付けられる。1つの外部コードは1つの施策にだけ紐づく。
 */
export function ExternalCodesCard({
  activityId,
  codes,
  canEdit,
  onChanged,
}: {
  activityId: number
  codes: ExternalCode[]
  canEdit: boolean
  onChanged: () => Promise<void>
}) {
  const [code, setCode] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [removing, setRemoving] = useState<ExternalCode | null>(null)

  const add = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.post(`/activities/${activityId}/external-codes`, { code, note })
      setCode('')
      setNote('')
      await onChanged()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="外部コード（案件番号）">
      <p className="mb-3 text-xs text-slate-500">
        会計・基幹システムで発行された案件番号などを、案件化したときに登録します。実績 CSV の <code className="font-mono">activity_code</code> に使えます。枠の施策には複数登録できます。
      </p>
      {codes.length === 0 ? (
        <p className="text-sm text-slate-400">未登録（計画段階）</p>
      ) : (
        <ul className="space-y-1.5">
          {codes.map((c) => (
            <li key={c.id} className="flex items-center gap-2 text-sm">
              <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-slate-800">{c.code}</span>
              {c.note && <span className="text-xs text-slate-500">{c.note}</span>}
              {canEdit && (
                <button type="button" onClick={() => setRemoving(c)} className="ml-auto rounded px-1 text-xs text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label={`${c.code} を外す`}>
                  外す
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canEdit && (
        <form onSubmit={add} className="mt-3 space-y-2 border-t border-slate-100 pt-3">
          <div className="flex gap-2">
            <Input aria-label="外部コード" value={code} onChange={(e) => setCode(e.target.value)} placeholder="例: P-2026-0123" className="font-mono" aria-invalid={fieldError(error, 'code') ? true : undefined} />
            <Button type="submit" disabled={busy || code.trim() === ''}>
              追加
            </Button>
          </div>
          <Input aria-label="外部コードのメモ" value={note} onChange={(e) => setNote(e.target.value)} placeholder="メモ（任意）例: A社 保守契約" />
          {fieldError(error, 'code') && <p className="text-xs text-red-600">{fieldError(error, 'code')}</p>}
          <FormError error={error} fields={['code', 'note']} />
        </form>
      )}
      <ConfirmDialog
        open={removing !== null}
        title="外部コードを外す"
        message={<>「{removing?.code}」をこの施策から外します。取込済みの実績は残ります。別の施策に付け替える場合は、外してから付け替え先で登録してください。</>}
        confirmLabel="外す"
        reason="optional"
        onClose={() => setRemoving(null)}
        onConfirm={async (reason) => {
          await api.del(`/activities/${activityId}/external-codes/${removing!.id}`, { reason })
          await onChanged()
        }}
      />
    </Card>
  )
}
