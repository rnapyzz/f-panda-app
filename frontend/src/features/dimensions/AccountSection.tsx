import { useEffect, useState, type FormEvent } from 'react'
import { accountsApi, type Account, type AccountType } from '../../api/dimensions'

const ACCOUNT_TYPE_LABELS: Record<AccountType, string> = {
  revenue: '収益',
  cost: '費用',
  other: 'その他',
}

export function AccountSection({ canEdit }: { canEdit: boolean }) {
  const [items, setItems] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newCode, setNewCode] = useState('')
  const [newName, setNewName] = useState('')
  const [newType, setNewType] = useState<AccountType>('cost')

  async function reload() {
    setLoading(true)
    try {
      setItems(await accountsApi.list())
      setError(null)
    } catch {
      setError('勘定科目一覧の取得に失敗しました')
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
      await accountsApi.create({ code: newCode, name: newName, account_type: newType })
      setNewCode('')
      setNewName('')
      await reload()
    } catch {
      setError('勘定科目の作成に失敗しました')
    }
  }

  async function toggleActive(a: Account) {
    try {
      await accountsApi.update(a.id, {
        code: a.code,
        name: a.name,
        account_type: a.account_type,
        is_active: !a.is_active,
      })
      await reload()
    } catch {
      setError('勘定科目の更新に失敗しました')
    }
  }

  if (loading) return <p>読み込み中...</p>

  return (
    <div>
      <h3>勘定科目</h3>
      {error && <p role="alert">{error}</p>}
      <table>
        <thead>
          <tr>
            <th>コード</th>
            <th>名称</th>
            <th>区分</th>
            <th>有効</th>
            {canEdit && <th />}
          </tr>
        </thead>
        <tbody>
          {items.map((a) => (
            <tr key={a.id}>
              <td>{a.code}</td>
              <td>{a.name}</td>
              <td>{ACCOUNT_TYPE_LABELS[a.account_type]}</td>
              <td>{a.is_active ? '有効' : '無効'}</td>
              {canEdit && (
                <td>
                  <button type="button" onClick={() => void toggleActive(a)}>
                    {a.is_active ? '無効化' : '有効化'}
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
          <select value={newType} onChange={(e) => setNewType(e.target.value as AccountType)}>
            <option value="revenue">収益</option>
            <option value="cost">費用</option>
            <option value="other">その他</option>
          </select>
          <button type="submit">勘定科目を追加</button>
        </form>
      )}
    </div>
  )
}
