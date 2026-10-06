import { useState, type ComponentProps, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { Activity, AllocationRule, GLAccount, List } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { CsvActions } from '../../components/CsvTransfer'
import { Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link } from '../../lib/router'
import { useApi } from '../../lib/useApi'

/**
 * 割当ルールの管理画面（docs/plan.md「2.12 実績の割当」）。
 * 箱の ID で施策が決まらない明細を、会計科目 × 部門で受け皿の施策に割り当てる。
 */
export function AllocationRulesPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const { data, error, loading, reload } = useApi<List<AllocationRule>>('/allocation-rules')
  const accounts = useApi<List<GLAccount>>('/gl-accounts')
  const activities = useApi<List<Activity>>('/activities')
  const [editing, setEditing] = useState<AllocationRule | 'new' | null>(null)
  const [deleting, setDeleting] = useState<AllocationRule | null>(null)

  return (
    <>
      <PageHeader
        title="割当ルール"
        description={
          <>
            案件番号などの箱の ID で施策が決まらない実績（人件費・減価償却・共通費など）を、会計科目 × 部門で受け皿の施策に割り当てます。部門を空にすると全部門に当たり、部門まで一致するルールが優先します。
            ルールを変えても取込済みの実績は変わりません。過去の月に当て直すときは<Link to="/admin/actuals" className="text-indigo-700 hover:underline">実績の割当</Link>の「再割当」を使います。
          </>
        }
        actions={
          <>
            <CsvActions
              resource="allocation-rules"
              label="割当ルール"
              canImport={canWrite}
              columns="account_code,department_code,activity_code"
              notes={<p>account_code は会計科目のコード、activity_code は施策コードです。department_code を空にすると全部門のルールになります。会計科目 × 部門が同じルールは、施策を更新します。</p>}
              onImported={reload}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ ルールを追加
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
        ) : !data || data.items.length === 0 ? (
          <Empty>割当ルールはありません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>会計科目</th>
                <th>部門</th>
                <th>割り当てる施策</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {data.items.map((r) => (
                <tr key={r.id}>
                  <td>
                    <span className="font-mono text-sm">{r.gl_account_code}</span> {r.gl_account_name}
                  </td>
                  <td>{r.department_code ? <span className="font-mono text-sm">{r.department_code}</span> : <span className="text-slate-500">全部門</span>}</td>
                  <td>
                    <Link to={`/activities/${r.activity_id}`} className="text-indigo-700 hover:underline">
                      {r.activity_name}
                    </Link>
                    <span className="ml-1 font-mono text-xs text-slate-400">{r.activity_code}</span>
                  </td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(r)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(r)}>
                        削除
                      </Button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {editing && accounts.data && activities.data && (
        <RuleDialog
          initial={editing === 'new' ? null : editing}
          accounts={accounts.data.items}
          activities={activities.data.items}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="割当ルールの削除"
        message={<>「{deleting?.gl_account_name} × {deleting?.department_code ?? '全部門'}」のルールを削除します。取込済みの実績の割当はそのまま残ります。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/allocation-rules/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

function RuleDialog({
  initial,
  accounts,
  activities,
  onClose,
  onSaved,
}: {
  initial: AllocationRule | null
  accounts: GLAccount[]
  activities: Activity[]
  onClose: () => void
  onSaved: () => void
}) {
  const [accountId, setAccountId] = useState(initial ? String(initial.gl_account_id) : '')
  const [dept, setDept] = useState(initial?.department_code ?? '')
  const [activityId, setActivityId] = useState(initial ? String(initial.activity_id) : '')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { gl_account_id: Number(accountId) || 0, department_code: dept, activity_id: Number(activityId) || 0, reason }
    try {
      if (initial) await api.put(`/allocation-rules/${initial.id}`, body)
      else await api.post('/allocation-rules', body)
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
      title={initial ? '割当ルールの編集' : '割当ルールの追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="rule-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="rule-form" onSubmit={submit} className="space-y-4">
        <Field label="会計科目" required error={fieldError(error, 'gl_account_id')}>
          {(p) => (
            <Select {...p} value={accountId} onChange={(e) => setAccountId(e.target.value)}>
              <option value="">選択してください</option>
              {accounts
                .filter((a) => !a.is_excluded)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.code} {a.name}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="部門コード" error={fieldError(error, 'department_code')} hint="会計システムの部門コード。空にすると全部門に当たります">
          {(p) => <Input {...p} value={dept} onChange={(e) => setDept(e.target.value)} className="font-mono" placeholder="例: D100" />}
        </Field>
        <Field label="割り当てる施策" required error={fieldError(error, 'activity_id')} hint="受け皿の施策（例: 本社共通費、開発部 基盤維持）">
          {(p) => <ActivitySelect {...p} activities={activities} value={activityId} onChange={setActivityId} />}
        </Field>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['gl_account_id', 'department_code', 'activity_id', 'reason']} />
      </form>
    </Dialog>
  )
}

/** 施策の選択。施策コードの順に並べる */
export function ActivitySelect({
  activities,
  value,
  onChange,
  ...rest
}: { activities: Activity[]; value: string; onChange: (v: string) => void } & Omit<ComponentProps<'select'>, 'value' | 'onChange'>) {
  const sorted = [...activities].sort((a, b) => a.code.localeCompare(b.code))
  return (
    <Select {...rest} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">選択してください</option>
      {sorted.map((a) => (
        <option key={a.id} value={a.id}>
          {a.code} {a.name}
        </option>
      ))}
    </Select>
  )
}
