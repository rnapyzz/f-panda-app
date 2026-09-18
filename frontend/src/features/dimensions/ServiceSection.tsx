import { useEffect, useState, type FormEvent } from 'react'
import { businessesApi, servicesApi, type Business, type Service } from '../../api/dimensions'
import { buttonSecondary, errorText, input, mutedText, select, sectionHeading, table, td, th } from '../../lib/ui'

export function ServiceSection({ canEdit }: { canEdit: boolean }) {
  const [items, setItems] = useState<Service[]>([])
  const [businesses, setBusinesses] = useState<Business[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newCode, setNewCode] = useState('')
  const [newName, setNewName] = useState('')
  const [newBusinessId, setNewBusinessId] = useState<number | ''>('')

  async function reload() {
    setLoading(true)
    try {
      const [services, businessList] = await Promise.all([servicesApi.list(), businessesApi.list()])
      setItems(services)
      setBusinesses(businessList)
      setError(null)
    } catch {
      setError('サービス一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
  }, [])

  const businessName = (id: number) => businesses.find((b) => b.id === id)?.name ?? `(#${id})`
  const activeBusinesses = businesses.filter((b) => b.is_active)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (newBusinessId === '') {
      setError('事業を選択してください')
      return
    }
    try {
      await servicesApi.create({ code: newCode, name: newName, business_id: newBusinessId })
      setNewCode('')
      setNewName('')
      await reload()
    } catch {
      setError('サービスの作成に失敗しました')
    }
  }

  async function toggleActive(s: Service) {
    try {
      await servicesApi.update(s.id, { code: s.code, name: s.name, business_id: s.business_id, is_active: !s.is_active })
      await reload()
    } catch {
      setError('サービスの更新に失敗しました')
    }
  }

  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h3 className={sectionHeading}>サービス</h3>
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
              <th className={th}>事業</th>
              <th className={th}>有効</th>
              {canEdit && <th className={th} />}
            </tr>
          </thead>
          <tbody>
            {items.map((s) => (
              <tr key={s.id}>
                <td className={td}>{s.code}</td>
                <td className={td}>{s.name}</td>
                <td className={td}>{businessName(s.business_id)}</td>
                <td className={td}>{s.is_active ? '有効' : '無効'}</td>
                {canEdit && (
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void toggleActive(s)}>
                      {s.is_active ? '無効化' : '有効化'}
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
          <select
            className={select}
            value={newBusinessId}
            onChange={(e) => setNewBusinessId(e.target.value ? Number(e.target.value) : '')}
            required
          >
            <option value="">事業を選択</option>
            {activeBusinesses.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
          <button type="submit" className={buttonSecondary}>
            サービスを追加
          </button>
        </form>
      )}
    </div>
  )
}
