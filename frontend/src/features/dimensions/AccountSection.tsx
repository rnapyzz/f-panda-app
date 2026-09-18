import { useEffect, useState, type FormEvent } from 'react'
import { accountsApi, type Account, type AccountType } from '../../api/dimensions'
import { buttonSecondary, errorText, input, mutedText, select, sectionHeading, table, td, th } from '../../lib/ui'

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

  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h3 className={sectionHeading}>勘定科目</h3>
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
              <th className={th}>区分</th>
              <th className={th}>有効</th>
              {canEdit && <th className={th} />}
            </tr>
          </thead>
          <tbody>
            {items.map((a) => (
              <tr key={a.id}>
                <td className={td}>{a.code}</td>
                <td className={td}>{a.name}</td>
                <td className={td}>{ACCOUNT_TYPE_LABELS[a.account_type]}</td>
                <td className={td}>{a.is_active ? '有効' : '無効'}</td>
                {canEdit && (
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void toggleActive(a)}>
                      {a.is_active ? '無効化' : '有効化'}
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
          <select className={select} value={newType} onChange={(e) => setNewType(e.target.value as AccountType)}>
            <option value="revenue">収益</option>
            <option value="cost">費用</option>
            <option value="other">その他</option>
          </select>
          <button type="submit" className={buttonSecondary}>
            勘定科目を追加
          </button>
        </form>
      )}
    </div>
  )
}
