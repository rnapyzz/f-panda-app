import { useEffect, useState, type FormEvent } from 'react'
import { departmentsApi, type Department } from '../../api/dimensions'
import { usersApi, type User, type UserRole } from '../../api/users'
import { useAuth } from '../auth/useAuth'
import { buttonPrimary, buttonSecondary, errorText, input, label as labelClass, mutedText, pageHeading, select, table, td, th } from '../../lib/ui'

const ROLE_LABELS: Record<UserRole, string> = {
  field_user: '現場担当者',
  office_admin: '事務局管理者',
}

export function UsersPage() {
  const { user } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [newEmail, setNewEmail] = useState('')
  const [newName, setNewName] = useState('')
  const [newRole, setNewRole] = useState<UserRole>('field_user')
  const [newDepartmentId, setNewDepartmentId] = useState<number | ''>('')
  const [newPassword, setNewPassword] = useState('')

  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editRole, setEditRole] = useState<UserRole>('field_user')
  const [editDepartmentId, setEditDepartmentId] = useState<number | ''>('')
  const [editIsActive, setEditIsActive] = useState(true)

  const [resetPasswordId, setResetPasswordId] = useState<number | null>(null)
  const [resetPasswordValue, setResetPasswordValue] = useState('')

  async function reload() {
    setLoading(true)
    try {
      const [u, d] = await Promise.all([usersApi.list(), departmentsApi.list()])
      setUsers(u)
      setDepartments(d)
      setError(null)
    } catch {
      setError('ユーザー一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
  }, [])

  const departmentName = (id?: number) => (id ? (departments.find((d) => d.id === id)?.name ?? `(#${id})`) : '—')
  const activeDepartments = departments.filter((d) => d.is_active)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      await usersApi.create({
        email: newEmail,
        name: newName,
        role: newRole,
        department_id: newDepartmentId === '' ? undefined : newDepartmentId,
        password: newPassword,
      })
      setNewEmail('')
      setNewName('')
      setNewRole('field_user')
      setNewDepartmentId('')
      setNewPassword('')
      await reload()
    } catch {
      setError('ユーザーの作成に失敗しました（メールアドレスの重複、パスワードが短すぎる等の可能性があります）')
    }
  }

  function startEdit(u: User) {
    setEditingId(u.id)
    setEditName(u.name)
    setEditRole(u.role)
    setEditDepartmentId(u.department_id ?? '')
    setEditIsActive(u.is_active)
  }

  async function handleSaveEdit(id: number) {
    setError(null)
    try {
      await usersApi.update(id, {
        name: editName,
        role: editRole,
        department_id: editDepartmentId === '' ? undefined : editDepartmentId,
        is_active: editIsActive,
      })
      setEditingId(null)
      await reload()
    } catch {
      setError('ユーザーの更新に失敗しました')
    }
  }

  async function handleResetPassword(id: number) {
    setError(null)
    if (resetPasswordValue.length < 8) {
      setError('パスワードは8文字以上で入力してください')
      return
    }
    try {
      await usersApi.resetPassword(id, resetPasswordValue)
      setResetPasswordId(null)
      setResetPasswordValue('')
    } catch {
      setError('パスワードのリセットに失敗しました')
    }
  }

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>ユーザー管理は事務局管理者のみ利用できます。</p>
  }
  if (loading) return <p className={mutedText}>読み込み中...</p>

  return (
    <div>
      <h1 className={pageHeading}>ユーザー管理</h1>
      <p className={mutedText}>
        アカウントを作成・編集します。パスワードは管理者が直接設定し、本人に別途連絡してください（メール招待の仕組みは未対応）。
      </p>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      <form onSubmit={handleCreate} className="my-4 flex flex-wrap items-end gap-3">
        <label className={labelClass}>
          メールアドレス
          <input type="email" className={input} value={newEmail} onChange={(e) => setNewEmail(e.target.value)} required />
        </label>
        <label className={labelClass}>
          氏名
          <input className={input} value={newName} onChange={(e) => setNewName(e.target.value)} required />
        </label>
        <label className={labelClass}>
          ロール
          <select className={select} value={newRole} onChange={(e) => setNewRole(e.target.value as UserRole)}>
            <option value="field_user">現場担当者</option>
            <option value="office_admin">事務局管理者</option>
          </select>
        </label>
        <label className={labelClass}>
          所属部門
          <select
            className={select}
            value={newDepartmentId}
            onChange={(e) => setNewDepartmentId(e.target.value ? Number(e.target.value) : '')}
          >
            <option value="">未設定</option>
            {activeDepartments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </label>
        <label className={labelClass}>
          初期パスワード
          <input
            type="password"
            className={input}
            value={newPassword}
            onChange={(e) => setNewPassword(e.target.value)}
            minLength={8}
            required
          />
        </label>
        <button type="submit" className={buttonPrimary}>
          ユーザーを作成
        </button>
      </form>

      <div className="overflow-x-auto">
        <table className={table}>
          <thead>
            <tr>
              <th className={th}>氏名</th>
              <th className={th}>メールアドレス</th>
              <th className={th}>ロール</th>
              <th className={th}>所属部門</th>
              <th className={th}>有効</th>
              <th className={th} />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id}>
                {editingId === u.id ? (
                  <>
                    <td className={td}>
                      <input className={input} value={editName} onChange={(e) => setEditName(e.target.value)} />
                    </td>
                    <td className={td}>{u.email}</td>
                    <td className={td}>
                      <select className={select} value={editRole} onChange={(e) => setEditRole(e.target.value as UserRole)}>
                        <option value="field_user">現場担当者</option>
                        <option value="office_admin">事務局管理者</option>
                      </select>
                    </td>
                    <td className={td}>
                      <select
                        className={select}
                        value={editDepartmentId}
                        onChange={(e) => setEditDepartmentId(e.target.value ? Number(e.target.value) : '')}
                      >
                        <option value="">未設定</option>
                        {activeDepartments.map((d) => (
                          <option key={d.id} value={d.id}>
                            {d.name}
                          </option>
                        ))}
                      </select>
                    </td>
                    <td className={td}>
                      <label className={labelClass}>
                        <input type="checkbox" checked={editIsActive} onChange={(e) => setEditIsActive(e.target.checked)} />
                        有効
                      </label>
                    </td>
                    <td className={td}>
                      <div className="flex gap-2">
                        <button type="button" className={buttonPrimary} onClick={() => void handleSaveEdit(u.id)}>
                          保存
                        </button>
                        <button type="button" className={buttonSecondary} onClick={() => setEditingId(null)}>
                          キャンセル
                        </button>
                      </div>
                    </td>
                  </>
                ) : (
                  <>
                    <td className={td}>{u.name}</td>
                    <td className={td}>{u.email}</td>
                    <td className={td}>{ROLE_LABELS[u.role]}</td>
                    <td className={td}>{departmentName(u.department_id)}</td>
                    <td className={td}>{u.is_active ? '有効' : '無効'}</td>
                    <td className={td}>
                      <div className="flex flex-wrap items-center gap-2">
                        <button type="button" className={buttonSecondary} onClick={() => startEdit(u)}>
                          編集
                        </button>
                        {resetPasswordId === u.id ? (
                          <>
                            <input
                              type="password"
                              className={`${input} w-32`}
                              placeholder="新パスワード"
                              value={resetPasswordValue}
                              onChange={(e) => setResetPasswordValue(e.target.value)}
                              minLength={8}
                            />
                            <button type="button" className={buttonPrimary} onClick={() => void handleResetPassword(u.id)}>
                              設定
                            </button>
                            <button
                              type="button"
                              className={buttonSecondary}
                              onClick={() => {
                                setResetPasswordId(null)
                                setResetPasswordValue('')
                              }}
                            >
                              キャンセル
                            </button>
                          </>
                        ) : (
                          <button type="button" className={buttonSecondary} onClick={() => setResetPasswordId(u.id)}>
                            パスワードリセット
                          </button>
                        )}
                      </div>
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
