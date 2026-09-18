import { useEffect, useState } from 'react'
import { auditLogApi, type AuditLogEntry } from '../../api/auditLog'
import { useAuth } from '../auth/useAuth'
import { buttonSecondary, errorText, input, label as labelClass, mutedText, pageHeading, table, td, th } from '../../lib/ui'

const PAGE_SIZE = 50

export function AuditLogPage() {
  const { user } = useAuth()
  const [logs, setLogs] = useState<AuditLogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [offset, setOffset] = useState(0)

  const [action, setAction] = useState('')
  const [entityType, setEntityType] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')

  async function reload() {
    setLoading(true)
    try {
      const rows = await auditLogApi.list({
        action: action || undefined,
        entity_type: entityType || undefined,
        from: from ? new Date(from).toISOString() : undefined,
        to: to ? new Date(to).toISOString() : undefined,
        limit: PAGE_SIZE,
        offset,
      })
      setLogs(rows)
      setError(null)
    } catch {
      setError('監査ログの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [offset])

  function handleFilterSubmit() {
    setOffset(0)
    void reload()
  }

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>監査ログは事務局管理者のみ利用できます。</p>
  }

  return (
    <div>
      <h1 className={pageHeading}>監査ログ</h1>
      <p className={mutedText}>ログイン・マスタ変更・提出・実績インポートなど、操作の履歴を確認できます。</p>

      <div className="my-4 flex flex-wrap items-end gap-3">
        <label className={labelClass}>
          アクション
          <input className={input} placeholder="例: create, submit" value={action} onChange={(e) => setAction(e.target.value)} />
        </label>
        <label className={labelClass}>
          対象種別
          <input className={input} placeholder="例: dim_business, submission" value={entityType} onChange={(e) => setEntityType(e.target.value)} />
        </label>
        <label className={labelClass}>
          開始日時
          <input type="datetime-local" className={input} value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label className={labelClass}>
          終了日時
          <input type="datetime-local" className={input} value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <button type="button" className={buttonSecondary} onClick={handleFilterSubmit}>
          絞り込み
        </button>
      </div>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      {loading ? (
        <p className={mutedText}>読み込み中...</p>
      ) : logs.length === 0 ? (
        <p className={mutedText}>該当するログはありません。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th className={th}>日時</th>
                <th className={th}>ユーザー</th>
                <th className={th}>アクション</th>
                <th className={th}>対象種別</th>
                <th className={th}>対象ID</th>
                <th className={th}>IPアドレス</th>
                <th className={th}>詳細</th>
              </tr>
            </thead>
            <tbody>
              {logs.map((l) => (
                <tr key={l.id}>
                  <td className={td}>{new Date(l.created_at).toLocaleString('ja-JP')}</td>
                  <td className={td}>{l.user_name ? `${l.user_name} (${l.user_email})` : '—'}</td>
                  <td className={td}>{l.action}</td>
                  <td className={td}>{l.entity_type}</td>
                  <td className={td}>{l.entity_id ?? '—'}</td>
                  <td className={td}>{l.ip_address ?? '—'}</td>
                  <td className={`${td} max-w-xs truncate`} title={JSON.stringify(l.detail)}>
                    {l.detail == null ? '—' : JSON.stringify(l.detail)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="mt-4 flex items-center gap-2">
        <button type="button" className={buttonSecondary} disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>
          前へ
        </button>
        <button type="button" className={buttonSecondary} disabled={logs.length < PAGE_SIZE} onClick={() => setOffset(offset + PAGE_SIZE)}>
          次へ
        </button>
      </div>
    </div>
  )
}
