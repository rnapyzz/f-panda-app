import { useEffect, useState, type FormEvent } from 'react'
import { assignmentsApi, type UserAssignment } from '../../api/assignments'
import { businessesApi, departmentsApi, type Business, type Department } from '../../api/dimensions'
import { usersApi, type User } from '../../api/users'
import { useAuth } from '../auth/useAuth'
import { buttonSecondary, errorText, label as labelClass, mutedText, pageHeading, select, table, td, th } from '../../lib/ui'

export function UserAssignmentsPage() {
  const { user } = useAuth()
  const [assignments, setAssignments] = useState<UserAssignment[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [businesses, setBusinesses] = useState<Business[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [userId, setUserId] = useState<number | ''>('')
  const [businessId, setBusinessId] = useState<number | ''>('')
  const [departmentId, setDepartmentId] = useState<number | ''>('')

  async function reload() {
    setLoading(true)
    try {
      const [a, u, b, d] = await Promise.all([
        assignmentsApi.list(),
        usersApi.list(),
        businessesApi.list(),
        departmentsApi.list(),
      ])
      setAssignments(a)
      setUsers(u.filter((x) => x.role === 'field_user'))
      setBusinesses(b)
      setDepartments(d)
      setError(null)
    } catch {
      setError('データの取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (userId === '' || businessId === '' || departmentId === '') return
    try {
      await assignmentsApi.create({ user_id: userId, business_id: businessId, department_id: departmentId })
      setUserId('')
      setBusinessId('')
      setDepartmentId('')
      await reload()
    } catch {
      setError('割当ての作成に失敗しました')
    }
  }

  async function handleDeactivate(id: number) {
    try {
      await assignmentsApi.deactivate(id)
      await reload()
    } catch {
      setError('割当ての解除に失敗しました')
    }
  }

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>担当割当て管理は事務局管理者のみ利用できます。</p>
  }
  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h1 className={pageHeading}>担当割当て管理</h1>
      <p className={mutedText}>現場担当者ごとに、担当する事業×部門の組み合わせを設定します。提出状況ダッシュボードはこの割当てを基準に未提出を判定します。</p>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      <form onSubmit={handleCreate} className="my-4 flex flex-wrap items-end gap-3">
        <label className={labelClass}>
          担当者
          <select className={select} value={userId} onChange={(e) => setUserId(e.target.value ? Number(e.target.value) : '')} required>
            <option value="">選択してください</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name} ({u.email})
              </option>
            ))}
          </select>
        </label>
        <label className={labelClass}>
          事業
          <select className={select} value={businessId} onChange={(e) => setBusinessId(e.target.value ? Number(e.target.value) : '')} required>
            <option value="">選択してください</option>
            {businesses.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </label>
        <label className={labelClass}>
          部門
          <select className={select} value={departmentId} onChange={(e) => setDepartmentId(e.target.value ? Number(e.target.value) : '')} required>
            <option value="">選択してください</option>
            {departments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className={buttonSecondary}>
          割当てを追加
        </button>
      </form>

      {assignments.length === 0 ? (
        <p className={mutedText}>割当てはまだありません。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th className={th}>担当者</th>
                <th className={th}>事業</th>
                <th className={th}>部門</th>
                <th className={th} />
              </tr>
            </thead>
            <tbody>
              {assignments.map((a) => (
                <tr key={a.id}>
                  <td className={td}>
                    {a.user_name} ({a.user_email})
                  </td>
                  <td className={td}>{a.business_name}</td>
                  <td className={td}>{a.department_name}</td>
                  <td className={td}>
                    <button type="button" className={buttonSecondary} onClick={() => void handleDeactivate(a.id)}>
                      解除
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
