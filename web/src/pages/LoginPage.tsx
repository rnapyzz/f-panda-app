import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import { useAuth } from '../lib/auth'
import { Button, ErrorMessage, Field, Input } from '../components/ui'

export function LoginPage() {
  const { login } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(email, password)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  const details = error instanceof ApiError ? error.details : {}
  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-sm">
        <h1 className="text-center text-2xl font-bold text-indigo-700">F-Panda</h1>
        <p className="mt-1 text-center text-sm text-slate-500">活動ベース予実管理 / ローリングフォアキャスト</p>
        <form onSubmit={submit} className="mt-8 space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-xs">
          <Field label="メールアドレス" error={details.email}>
            {(p) => <Input {...p} type="email" autoComplete="username" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />}
          </Field>
          <Field label="パスワード" error={details.password}>
            {(p) => <Input {...p} type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
          {error instanceof ApiError && Object.keys(details).length === 0 ? <ErrorMessage error={error} /> : null}
          {error && !(error instanceof ApiError) ? <ErrorMessage error="サーバーに接続できませんでした" /> : null}
          <Button type="submit" variant="primary" className="w-full" disabled={busy}>
            {busy ? 'ログイン中…' : 'ログイン'}
          </Button>
        </form>
      </div>
    </div>
  )
}
