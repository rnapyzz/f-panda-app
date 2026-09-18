import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError } from '../../api/client'
import { buttonPrimary, errorText, input, label, pageHeading } from '../../lib/ui'
import { useAuth } from './useAuth'

export function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(email, password)
      navigate('/')
    } catch (err) {
      setError(err instanceof ApiError ? 'メールアドレスまたはパスワードが正しくありません' : '通信エラーが発生しました')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto mt-24 max-w-xs px-4">
      <h1 className={pageHeading}>ログイン</h1>
      <form onSubmit={handleSubmit} className="space-y-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="email" className={label}>
            メールアドレス
          </label>
          <input
            id="email"
            type="email"
            className={input}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor="password" className={label}>
            パスワード
          </label>
          <input
            id="password"
            type="password"
            className={input}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </div>
        {error && (
          <p role="alert" className={errorText}>
            {error}
          </p>
        )}
        <button type="submit" className={buttonPrimary} disabled={submitting}>
          {submitting ? 'ログイン中...' : 'ログイン'}
        </button>
      </form>
    </div>
  )
}
