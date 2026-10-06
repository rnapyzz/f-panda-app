import { useEffect, useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { RefreshActualsResult, Scenario } from '../../api/types'
import { Button, Dialog, ErrorMessage, Field, Loading, Textarea, fieldError } from '../../components/ui'
import { DriftTable } from './DriftTable'

/**
 * ロック済みのシナリオの実績を最新にする（docs/plan.md「2.14」）。
 * 開くと月ごとの差を確認（dry run）し、理由を入れて実行すると、シナリオに保存した実績を今の実績に入れ替える。ロックは外れない。
 */
export function RefreshActualsDialog({ scenario, onClose, onDone }: { scenario: Scenario; onClose: () => void; onDone: () => Promise<unknown> }) {
  const path = `/scenarios/${scenario.id}/refresh-actuals`
  const [preview, setPreview] = useState<RefreshActualsResult | null>(null)
  const [done, setDone] = useState<RefreshActualsResult | null>(null)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.post<RefreshActualsResult>(path, { dry_run: true }).then(setPreview, setError)
  }, [path])

  const run = async () => {
    setBusy(true)
    setError(null)
    try {
      setDone(await api.post<RefreshActualsResult>(path, { reason }))
      await onDone()
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
      title="実績を最新にする"
      onClose={onClose}
      footer={
        done ? (
          <Button variant="primary" onClick={onClose}>
            閉じる
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>キャンセル</Button>
            <Button variant="primary" disabled={busy || !preview || !reason.trim()} onClick={run}>
              {busy ? '処理中…' : '最新にする'}
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-slate-600">
          「{scenario.name}」（ロック済み）に保存した決算確定月以前の実績を、今の実績に入れ替えます。ロックは外れず、計画値の月は変わりません。比較やホームの数字が変わります。
        </p>
        {done ? (
          <p className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-800" role="status">
            実績を最新にしました（{done.saved} 件）。
          </p>
        ) : error && !(error instanceof ApiError && error.details.reason) ? (
          <ErrorMessage error={error} />
        ) : !preview ? (
          <Loading />
        ) : null}
        {(done ?? preview) && <DriftTable months={(done ?? preview)!.drift.months} />}
        {!done && preview && (
          <Field label="変更理由" required error={fieldError(error, 'reason')} hint="例: 8月の決算整理（賞与引当金）を反映">
            {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
          </Field>
        )}
      </div>
    </Dialog>
  )
}
