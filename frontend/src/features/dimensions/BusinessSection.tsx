import { useEffect, useState, type FormEvent } from 'react'
import { businessesApi, type Business } from '../../api/dimensions'
import { buttonSecondary, errorText, input, mutedText, sectionHeading, table, td, th } from '../../lib/ui'

export function BusinessSection({ canEdit }: { canEdit: boolean }) {
  const [items, setItems] = useState<Business[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newCode, setNewCode] = useState('')
  const [newName, setNewName] = useState('')

  async function reload() {
    setLoading(true)
    try {
      setItems(await businessesApi.list())
      setError(null)
    } catch {
      setError('事業一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    try {
      await businessesApi.create({ code: newCode, name: newName })
      setNewCode('')
      setNewName('')
      await reload()
    } catch {
      setError('事業の作成に失敗しました')
    }
  }

  async function toggleActive(b: Business) {
    try {
      await businessesApi.update(b.id, { code: b.code, name: b.name, is_active: !b.is_active })
      await reload()
    } catch {
      setError('事業の更新に失敗しました')
    }
  }

  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h3 className={sectionHeading}>事業</h3>
      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}
      <div className="overflow-x-auto">
        <table className={table}>
          <thead>
            <tr>
              <th className={th}>コード</th>
              <th className={th}>名称</th>
              <th className={th}>有効</th>
              {canEdit && <th className={th} />}
            </tr>
          </thead>
          <tbody>
            {items.map((b) => (
              <tr key={b.id}>
                <td className={td}>{b.code}</td>
                <td className={td}>{b.name}</td>
                <td className={td}>{b.is_active ? '有効' : '無効'}</td>
                {canEdit && (
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void toggleActive(b)}>
                      {b.is_active ? '無効化' : '有効化'}
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {canEdit && (
        <form onSubmit={handleCreate} className="mt-2 flex flex-wrap items-center gap-2">
          <input className={input} placeholder="コード" value={newCode} onChange={(e) => setNewCode(e.target.value)} required />
          <input className={input} placeholder="名称" value={newName} onChange={(e) => setNewName(e.target.value)} required />
          <button type="submit" className={buttonSecondary}>
            事業を追加
          </button>
        </form>
      )}
    </div>
  )
}
