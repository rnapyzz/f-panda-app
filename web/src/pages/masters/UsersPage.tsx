import { CsvActions } from '../../components/CsvTransfer'
import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { roleLabels, type List, type Role, type User } from '../../api/types'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { useApi } from '../../lib/useApi'

const roles: Role[] = ['fpa_admin', 'manager', 'member', 'viewer']

/** ユーザーの管理画面 */
export function UsersPage() {
  const me = useCurrentUser()
  const canWrite = me.role === 'fpa_admin'
  const { data, error, loading, reload } = useApi<List<User>>('/users')
  const [editing, setEditing] = useState<User | 'new' | null>(null)
  const [resetting, setResetting] = useState<User | null>(null)

  return (
    <>
      <PageHeader
        title="ユーザー"
        description="ユーザーは削除せず、無効化するとログインできなくなります。"
        actions={
          <>
            <CsvActions
              resource="users"
              label="ユーザー"
              canImport={canWrite}
              columns="email,name,role,is_active,slack_user_id"
              notes={<p>role は fpa_admin / manager / member / viewer。パスワードは CSV では扱いません。追加したユーザーは「パスワード未設定」になるので、「パスワード再設定」から設定してください。slack_user_id（Slack のメンバー ID）は列ごと省略でき、省略すると登録済みの ID を変えません。</p>}
              onImported={reload}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ ユーザーを追加
              </Button>
            )}
          </>
        }
      />
      <Card>
        {loading && !data ? (
          <Loading />
        ) : error ? (
          <ErrorMessage error={error} />
        ) : data!.items.length === 0 ? (
          <Empty>ユーザーが登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>氏名</th>
                <th>メールアドレス</th>
                <th>ロール</th>
                <th>Slack</th>
                <th>状態</th>
                {canWrite && <th className="w-48" />}
              </tr>
            </thead>
            <tbody>
              {data!.items.map((u) => (
                <tr key={u.id} className={u.is_active ? '' : 'text-slate-400'}>
                  <td className="font-medium">
                    {u.name}
                    {u.id === me.id && <span className="ml-1 text-xs text-slate-400">（自分）</span>}
                  </td>
                  <td>{u.email}</td>
                  <td>{roleLabels[u.role]}</td>
                  <td className="font-mono text-xs">{u.slack_user_id || <span className="font-sans text-slate-300">—</span>}</td>
                  <td className="space-x-1">
                    {u.is_active ? <Badge tone="green">有効</Badge> : <Badge>無効</Badge>}
                    {!u.has_password && <Badge tone="amber">パスワード未設定</Badge>}
                  </td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(u)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setResetting(u)}>
                        パスワード再設定
                      </Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {editing && (
        <UserDialog
          initial={editing === 'new' ? null : editing}
          isSelf={editing !== 'new' && editing.id === me.id}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      {resetting && <PasswordDialog user={resetting} onClose={() => setResetting(null)} />}
    </>
  )
}

function UserDialog({ initial, isSelf, onClose, onSaved }: { initial: User | null; isSelf: boolean; onClose: () => void; onSaved: () => void }) {
  const [name, setName] = useState(initial?.name ?? '')
  const [email, setEmail] = useState(initial?.email ?? '')
  const [role, setRole] = useState<Role>(initial?.role ?? 'member')
  const [isActive, setIsActive] = useState(initial?.is_active ?? true)
  const [slackId, setSlackId] = useState(initial?.slack_user_id ?? '')
  const [password, setPassword] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      if (initial) await api.put(`/users/${initial.id}`, { name, email, role, is_active: isActive, slack_user_id: slackId, reason })
      else await api.post('/users', { name, email, role, password, slack_user_id: slackId, reason })
      onSaved()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title={initial ? 'ユーザーの編集' : 'ユーザーの追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="user-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="user-form" onSubmit={submit} className="space-y-4">
        <Field label="氏名" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <Field label="メールアドレス" required error={fieldError(error, 'email')}>
          {(p) => <Input {...p} type="email" value={email} onChange={(e) => setEmail(e.target.value)} />}
        </Field>
        <Field label="ロール" required error={fieldError(error, 'role')} hint={isSelf ? '自分自身のロールは変更できません' : undefined}>
          {(p) => (
            <Select {...p} value={role} disabled={isSelf} onChange={(e) => setRole(e.target.value as Role)}>
              {roles.map((r) => (
                <option key={r} value={r}>
                  {roleLabels[r]}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field
          label="Slack のメンバー ID"
          error={fieldError(error, 'slack_user_id')}
          hint="通知で @メンションするための ID（例: U012AB3CD）。Slack のプロフィールの「⋮ → メンバー ID をコピー」で確認できます"
        >
          {(p) => <Input {...p} value={slackId} onChange={(e) => setSlackId(e.target.value.trim())} className="w-48 font-mono" />}
        </Field>
        {initial ? (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={isActive} disabled={isSelf} onChange={(e) => setIsActive(e.target.checked)} className="size-4 rounded border-slate-300" />
            有効（チェックを外すとログインできなくなります）
          </label>
        ) : (
          <Field label="初期パスワード" required error={fieldError(error, 'password')} hint="12文字以上">
            {(p) => <Input {...p} type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
        )}
        {fieldError(error, 'is_active') && <p className="text-xs text-red-600">{fieldError(error, 'is_active')}</p>}
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['name', 'email', 'role', 'password', 'is_active', 'slack_user_id', 'reason']} />
      </form>
    </Dialog>
  )
}

function PasswordDialog({ user, onClose }: { user: User; onClose: () => void }) {
  const [password, setPassword] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.put(`/users/${user.id}/password`, { password, reason })
      setDone(true)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title="パスワードの再設定"
      onClose={onClose}
      footer={
        done ? (
          <Button variant="primary" onClick={onClose}>
            閉じる
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>キャンセル</Button>
            <Button variant="primary" type="submit" form="password-form" disabled={busy}>
              {busy ? '保存中…' : '再設定'}
            </Button>
          </>
        )
      }
    >
      {done ? (
        <p className="text-sm text-slate-700">{user.name} さんのパスワードを再設定しました。ログイン中のセッションはすべて終了しています。新しいパスワードを本人に伝えてください。</p>
      ) : (
        <form id="password-form" onSubmit={submit} className="space-y-4">
          <p className="text-sm text-slate-600">{user.name}（{user.email}）のパスワードを再設定します。</p>
          <Field label="新しいパスワード" required error={fieldError(error, 'password')} hint="12文字以上">
            {(p) => <Input {...p} type="password" autoComplete="new-password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />}
          </Field>
          <Field label="変更理由（任意）">{(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}</Field>
          <FormError error={error} fields={['password']} />
        </form>
      )}
    </Dialog>
  )
}
