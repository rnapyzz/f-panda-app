import { useEffect, useState, type FormEvent } from 'react'
import { departmentsApi, type Department } from '../../api/dimensions'
import { buttonSecondary, errorText, input, mutedText, sectionHeading, table, td, th } from '../../lib/ui'

export function DepartmentSection({ canEdit }: { canEdit: boolean }) {
  const [items, setItems] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newCode, setNewCode] = useState('')
  const [newName, setNewName] = useState('')

  async function reload() {
    setLoading(true)
    try {
      setItems(await departmentsApi.list())
      setError(null)
    } catch {
      setError('部門一覧の取得に失敗しました')
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
      await departmentsApi.create({ code: newCode, name: newName })
      setNewCode('')
      setNewName('')
      await reload()
    } catch {
      setError('部門の作成に失敗しました')
    }
  }

  async function toggleActive(d: Department) {
    try {
      await departmentsApi.update(d.id, { code: d.code, name: d.name, is_active: !d.is_active })
      await reload()
    } catch {
      setError('部門の更新に失敗しました')
    }
  }

  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h3 className={sectionHeading}>部門</h3>
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
            {items.map((d) => (
              <tr key={d.id}>
                <td className={td}>{d.code}</td>
                <td className={td}>{d.name}</td>
                <td className={td}>{d.is_active ? '有効' : '無効'}</td>
                {canEdit && (
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void toggleActive(d)}>
                      {d.is_active ? '無効化' : '有効化'}
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
            部門を追加
          </button>
        </form>
      )}
    </div>
  )
}
