import { useEffect, useState, type FormEvent } from 'react'
import { businessesApi, type Business } from '../../api/dimensions'

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

  if (loading) return <p>読み込み中...</p>

  return (
    <div>
      <h3>事業</h3>
      {error && <p role="alert">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>コード</th>
            <th>名称</th>
            <th>有効</th>
            {canEdit && <th />}
          </tr>
        </thead>
        <tbody>
          {items.map((b) => (
            <tr key={b.id}>
              <td>{b.code}</td>
              <td>{b.name}</td>
              <td>{b.is_active ? '有効' : '無効'}</td>
              {canEdit && (
                <td>
                  <button type="button" onClick={() => void toggleActive(b)}>
                    {b.is_active ? '無効化' : '有効化'}
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {canEdit && (
        <form onSubmit={handleCreate} style={{ marginTop: 8 }}>
          <input placeholder="コード" value={newCode} onChange={(e) => setNewCode(e.target.value)} required />
          <input placeholder="名称" value={newName} onChange={(e) => setNewName(e.target.value)} required />
          <button type="submit">事業を追加</button>
        </form>
      )}
    </div>
  )
}
