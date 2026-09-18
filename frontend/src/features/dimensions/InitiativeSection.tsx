import { useEffect, useState, type FormEvent } from 'react'
import { departmentsApi, initiativesApi, servicesApi, type Department, type Initiative, type Service } from '../../api/dimensions'
import { buttonSecondary, errorText, input, mutedText, select, sectionHeading, table, td, th } from '../../lib/ui'

export function InitiativeSection({ canEdit }: { canEdit: boolean }) {
  const [items, setItems] = useState<Initiative[]>([])
  const [services, setServices] = useState<Service[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [newCode, setNewCode] = useState('')
  const [newName, setNewName] = useState('')
  const [newServiceId, setNewServiceId] = useState<number | ''>('')
  const [newDepartmentId, setNewDepartmentId] = useState<number | ''>('')

  async function reload() {
    setLoading(true)
    try {
      const [initiatives, serviceList, departmentList] = await Promise.all([
        initiativesApi.list(),
        servicesApi.list(),
        departmentsApi.list(),
      ])
      setItems(initiatives)
      setServices(serviceList)
      setDepartments(departmentList)
      setError(null)
    } catch {
      setError('施策一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
  }, [])

  const serviceName = (id: number) => services.find((s) => s.id === id)?.name ?? `(#${id})`
  const departmentName = (id: number) => departments.find((d) => d.id === id)?.name ?? `(#${id})`
  const activeServices = services.filter((s) => s.is_active)
  const activeDepartments = departments.filter((d) => d.is_active)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (newServiceId === '' || newDepartmentId === '') {
      setError('サービスと主部門を選択してください')
      return
    }
    try {
      await initiativesApi.create({ code: newCode, name: newName, service_id: newServiceId, primary_department_id: newDepartmentId })
      setNewCode('')
      setNewName('')
      await reload()
    } catch {
      setError('施策の作成に失敗しました')
    }
  }

  async function toggleActive(i: Initiative) {
    try {
      await initiativesApi.update(i.id, {
        code: i.code,
        name: i.name,
        service_id: i.service_id,
        primary_department_id: i.primary_department_id,
        is_active: !i.is_active,
      })
      await reload()
    } catch {
      setError('施策の更新に失敗しました')
    }
  }

  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h3 className={sectionHeading}>施策</h3>
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
              <th className={th}>サービス</th>
              <th className={th}>主部門</th>
              <th className={th}>有効</th>
              {canEdit && <th className={th} />}
            </tr>
          </thead>
          <tbody>
            {items.map((i) => (
              <tr key={i.id}>
                <td className={td}>{i.code}</td>
                <td className={td}>{i.name}</td>
                <td className={td}>{serviceName(i.service_id)}</td>
                <td className={td}>{departmentName(i.primary_department_id)}</td>
                <td className={td}>{i.is_active ? '有効' : '無効'}</td>
                {canEdit && (
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void toggleActive(i)}>
                      {i.is_active ? '無効化' : '有効化'}
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
            value={newServiceId}
            onChange={(e) => setNewServiceId(e.target.value ? Number(e.target.value) : '')}
            required
          >
            <option value="">サービスを選択</option>
            {activeServices.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
          <select
            className={select}
            value={newDepartmentId}
            onChange={(e) => setNewDepartmentId(e.target.value ? Number(e.target.value) : '')}
            required
          >
            <option value="">主部門を選択</option>
            {activeDepartments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
          <button type="submit" className={buttonSecondary}>
            施策を追加
          </button>
        </form>
      )}
    </div>
  )
}
