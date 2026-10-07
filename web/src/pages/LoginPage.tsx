import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import { useAuth } from '../lib/auth'
import { Button, ErrorMessage, Field, Input } from '../components/ui'

type AuthConfig = { sso: boolean; password_roles: string[] }

/** SSO のログインに失敗して戻ってきたときの理由（/?login_error=...） */
const ssoErrors: Record<string, string> = {
  not_registered: 'このアカウントは F-Panda に登録されていません。FP&A に登録を依頼してください。',
  domain: 'このアカウントのドメインではログインできません。会社の Google アカウントでログインしてください。',
  failed: 'Google でのログインに失敗しました。もう一度お試しください。',
}

/**
 * ログイン画面。SSO（Google Workspace）が有効な環境では「Google でログイン」を出し、
 * パスワードでのログインは FP&A の非常用として折りたたむ（docs/architecture.md「認証」）。
 */
export function LoginPage() {
  const [config, setConfig] = useState<AuthConfig | null>(null)
  const [emergency, setEmergency] = useState(false)
  const ssoError = new URLSearchParams(window.location.search).get('login_error')

  useEffect(() => {
    api.get<AuthConfig>('/auth/config').then(setConfig, () => setConfig({ sso: false, password_roles: [] }))
  }, [])

  // SSO の後は、開いていた画面（ログイン画面を出したときのパス）へ戻る
  const returnTo = window.location.pathname === '/' ? '/' : window.location.pathname + window.location.search
  const ssoHref = `/api/auth/oidc/login?return_to=${encodeURIComponent(returnTo)}`

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-sm">
        <img src="/favicon.svg" alt="" width={64} height={64} className="mx-auto mb-3" />
        <h1 className="text-center text-2xl font-bold text-indigo-700">F-Panda</h1>
        <p className="mt-1 text-center text-sm text-slate-500">活動ベース予実管理 / ローリングフォアキャスト</p>
        <div className="mt-8 space-y-4 rounded-lg border border-slate-200 bg-white p-6 shadow-xs">
          {ssoError && <ErrorMessage error={ssoErrors[ssoError] ?? ssoErrors.failed} />}
          {config?.sso ? (
            <>
              <a
                href={ssoHref}
                className="flex w-full items-center justify-center gap-2 rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 shadow-xs hover:bg-slate-50"
              >
                <svg viewBox="0 0 24 24" className="size-5" aria-hidden="true">
                  <path fill="#4285F4" d="M22.5 12.3c0-.8-.1-1.5-.2-2.3H12v4.3h5.9a5 5 0 0 1-2.2 3.3v2.7h3.5c2.1-1.9 3.3-4.7 3.3-8Z" />
                  <path fill="#34A853" d="M12 23c3 0 5.5-1 7.3-2.7l-3.5-2.7c-1 .7-2.3 1-3.8 1-2.9 0-5.4-2-6.3-4.6H2.1v2.8A11 11 0 0 0 12 23Z" />
                  <path fill="#FBBC05" d="M5.7 14c-.2-.7-.4-1.4-.4-2s.2-1.4.4-2V7.2H2.1a11 11 0 0 0 0 9.6L5.7 14Z" />
                  <path fill="#EA4335" d="M12 5.4c1.6 0 3.1.6 4.2 1.7l3.2-3.2A11 11 0 0 0 2.1 7.2L5.7 10c.9-2.6 3.4-4.6 6.3-4.6Z" />
                </svg>
                Google でログイン
              </a>
              {emergency ? (
                <PasswordForm note="非常用のログインです。FP&A のアカウントだけが使えます。" />
              ) : (
                <button type="button" onClick={() => setEmergency(true)} className="block w-full text-center text-xs text-slate-500 hover:underline">
                  非常用のログイン（FP&A）
                </button>
              )}
            </>
          ) : config ? (
            <PasswordForm />
          ) : null}
        </div>
      </div>
    </div>
  )
}

function PasswordForm({ note }: { note?: string }) {
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
    <form onSubmit={submit} className="space-y-4">
      {note && <p className="text-xs text-slate-500">{note}</p>}
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
  )
}
