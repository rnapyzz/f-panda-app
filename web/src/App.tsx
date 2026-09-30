import { useEffect, useState } from 'react'

type Health = { status: string; database: string }

function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then(async (res) => {
        const body = (await res.json()) as Health
        setHealth(body)
      })
      .catch((err: unknown) => setError(String(err)))
  }, [])

  return (
    <main className="min-h-screen bg-slate-50 p-8 text-slate-800">
      <h1 className="text-2xl font-bold">F-Panda</h1>
      <p className="mt-1 text-sm text-slate-500">活動ベース予実管理 / ローリングフォアキャスト</p>

      <section className="mt-6 rounded-lg border border-slate-200 bg-white p-4">
        <h2 className="text-sm font-semibold">API ステータス</h2>
        {error && <p className="mt-2 text-sm text-red-600">接続エラー: {error}</p>}
        {!error && !health && <p className="mt-2 text-sm text-slate-500">確認中…</p>}
        {health && (
          <dl className="mt-2 grid grid-cols-[8rem_1fr] gap-y-1 text-sm">
            <dt className="text-slate-500">API</dt>
            <dd>{health.status}</dd>
            <dt className="text-slate-500">データベース</dt>
            <dd>{health.database}</dd>
          </dl>
        )}
      </section>
    </main>
  )
}

export default App
