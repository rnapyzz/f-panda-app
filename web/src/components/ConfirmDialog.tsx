import { useState, type ReactNode } from 'react'
import { ApiError } from '../api/client'
import { Button, Dialog, ErrorMessage, Field, Textarea } from './ui'

/**
 * 削除などの確認ダイアログ。reason を 'optional' / 'required' にすると変更理由の入力欄を出す。
 * onConfirm が投げたエラーはダイアログ内に表示する。
 */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = '削除',
  danger = true,
  reason,
  onConfirm,
  onClose,
}: {
  open: boolean
  title: string
  message: ReactNode
  confirmLabel?: string
  danger?: boolean
  reason?: 'optional' | 'required'
  onConfirm: (reason: string) => Promise<void>
  onClose: () => void
}) {
  const [text, setText] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const close = () => {
    setText('')
    setError(null)
    onClose()
  }
  const confirm = async () => {
    setBusy(true)
    setError(null)
    try {
      await onConfirm(text.trim())
      setText('')
      onClose()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open={open}
      title={title}
      onClose={close}
      footer={
        <>
          <Button onClick={close}>キャンセル</Button>
          <Button variant={danger ? 'danger' : 'primary'} disabled={busy || (reason === 'required' && text.trim() === '')} onClick={confirm}>
            {busy ? '処理中…' : confirmLabel}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        <div className="text-sm text-slate-700">{message}</div>
        {reason && (
          <Field label={reason === 'required' ? '変更理由' : '変更理由（任意）'} required={reason === 'required'} error={error instanceof ApiError ? error.details.reason : undefined}>
            {(p) => <Textarea {...p} value={text} onChange={(e) => setText(e.target.value)} />}
          </Field>
        )}
        {error instanceof ApiError && error.details.reason ? null : <ErrorMessage error={error} />}
      </div>
    </Dialog>
  )
}
