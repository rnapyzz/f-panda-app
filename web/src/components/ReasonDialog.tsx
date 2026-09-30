import { useCallback, useRef, useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import { Button, Dialog, ErrorMessage, Field, Textarea } from './ui'

type Pending = {
  title: string
  run: (reason: string) => Promise<void>
  resolve: (ok: boolean) => void
}

/**
 * 変更理由が必要な操作を実行するためのフック。
 *
 * withReason(op) は、まず op(undefined) を実行する。API が「変更理由が必要」（422 details.reason）を
 * 返したら理由の入力ダイアログを出し、入力された理由で op(reason) を再実行する。
 * askReason(title, op) は、最初から理由を入力してもらう（削除など、必ず理由が要る操作用）。
 * いずれも、完了したら true、キャンセルしたら false を返す。それ以外のエラーはそのまま投げる。
 */
export function useReason() {
  const [pending, setPending] = useState<Pending | null>(null)

  const askReason = useCallback((title: string, run: (reason: string) => Promise<void>) => {
    return new Promise<boolean>((resolve) => setPending({ title, run, resolve }))
  }, [])

  const withReason = useCallback(
    async (run: (reason?: string) => Promise<void>, title = '変更理由を入力してください'): Promise<boolean> => {
      try {
        await run(undefined)
        return true
      } catch (err) {
        if (err instanceof ApiError && err.needsReason) {
          return askReason(title, run)
        }
        throw err
      }
    },
    [askReason],
  )

  const dialog = pending ? (
    <ReasonDialog
      title={pending.title}
      onSubmit={pending.run}
      onDone={(ok) => {
        pending.resolve(ok)
        setPending(null)
      }}
    />
  ) : null

  return { withReason, askReason, dialog }
}

function ReasonDialog({ title, onSubmit, onDone }: { title: string; onSubmit: (reason: string) => Promise<void>; onDone: (ok: boolean) => void }) {
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const formRef = useRef<HTMLFormElement>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await onSubmit(reason.trim())
      onDone(true)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title={title}
      onClose={() => onDone(false)}
      footer={
        <>
          <Button onClick={() => onDone(false)}>キャンセル</Button>
          <Button variant="primary" disabled={busy || reason.trim() === ''} onClick={() => formRef.current?.requestSubmit()}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form ref={formRef} onSubmit={submit} className="space-y-3">
        <p className="text-sm text-slate-600">この変更は、理由を記録してから保存します。変更履歴に残り、後から確認できます。</p>
        <Field label="変更理由" required error={error instanceof ApiError ? error.details.reason : undefined}>
          {(p) => <Textarea {...p} autoFocus value={reason} onChange={(e) => setReason(e.target.value)} placeholder="例: 受注確度の見直し（A社の稟議が通過）" />}
        </Field>
        {error instanceof ApiError && !error.details.reason ? <ErrorMessage error={error} /> : null}
        {error && !(error instanceof ApiError) ? <ErrorMessage error={error} /> : null}
      </form>
    </Dialog>
  )
}
