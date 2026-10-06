import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { categoryLabels, type GLAccount, type List, type Subject } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { CsvActions } from '../../components/CsvTransfer'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { useApi } from '../../lib/useApi'

/**
 * 会計科目の管理画面（docs/plan.md「2.12 実績の割当」）。
 * 会計システムの勘定科目を、アプリの科目に対応させる。P/L に関係ない会計科目は対象外にする。
 */
export function GLAccountsPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const { data, error, loading, reload } = useApi<List<GLAccount>>('/gl-accounts')
  const subjects = useApi<List<Subject>>('/subjects')
  const [editing, setEditing] = useState<GLAccount | 'new' | null>(null)
  const [deleting, setDeleting] = useState<GLAccount | null>(null)
  const subjectById = new Map((subjects.data?.items ?? []).map((s) => [s.id, s]))

  return (
    <>
      <PageHeader
        title="会計科目"
        description="会計システムの勘定科目です。実績の取込で、明細の会計科目をアプリの科目に読み替えます。P/L に関係ない会計科目（預金など）は「対象外」にすると、取り込みません。"
        actions={
          <>
            <CsvActions
              resource="gl-accounts"
              label="会計科目"
              canImport={canWrite}
              columns="code,name,subject_code,hide_details"
              notes={<p>subject_code はアプリの科目のコードです。空にすると対象外になります。hide_details は true / false（明細を FP&A 以外に見せない）。</p>}
              onImported={reload}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ 会計科目を追加
              </Button>
            )}
          </>
        }
      />
      <Card>
        {loading && !data ? (
          <Loading />
        ) : error || subjects.error ? (
          <ErrorMessage error={error ?? subjects.error} />
        ) : !data || data.items.length === 0 ? (
          <Empty>会計科目が登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-28">コード</th>
                <th>名前</th>
                <th>アプリの科目</th>
                <th>明細</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {data.items.map((g) => {
                const s = g.subject_id ? subjectById.get(g.subject_id) : undefined
                return (
                  <tr key={g.id}>
                    <td className="font-mono text-sm">{g.code}</td>
                    <td className="font-medium">{g.name}</td>
                    <td>
                      {g.is_excluded ? (
                        <Badge tone="slate">対象外</Badge>
                      ) : (
                        <span>
                          {s?.name}
                          {s && <span className="ml-1 text-xs text-slate-400">（{s.code}・{categoryLabels[s.category]}）</span>}
                        </span>
                      )}
                    </td>
                    <td>{g.hide_details ? <Badge tone="amber">FP&A のみ</Badge> : <span className="text-xs text-slate-500">全員</span>}</td>
                    {canWrite && (
                      <td className="text-right">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(g)}>
                          編集
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setDeleting(g)}>
                          削除
                        </Button>
                      </td>
                    )}
                  </tr>
                )
              })}
            </tbody>
          </Table>
        )}
      </Card>

      {editing && subjects.data && (
        <AccountDialog
          initial={editing === 'new' ? null : editing}
          subjects={subjects.data.items}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="会計科目の削除"
        message={<>「{deleting?.code} {deleting?.name}」を削除します。取り込んだ明細や割当ルールで使われている会計科目は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/gl-accounts/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

function AccountDialog({ initial, subjects, onClose, onSaved }: { initial: GLAccount | null; subjects: Subject[]; onClose: () => void; onSaved: () => void }) {
  const [code, setCode] = useState(initial?.code ?? '')
  const [name, setName] = useState(initial?.name ?? '')
  const [subjectId, setSubjectId] = useState(initial?.subject_id ? String(initial.subject_id) : '')
  const [excluded, setExcluded] = useState(initial?.is_excluded ?? false)
  const [hide, setHide] = useState(initial?.hide_details ?? false)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { code, name, subject_id: excluded || !subjectId ? null : Number(subjectId), is_excluded: excluded, hide_details: hide, reason }
    try {
      if (initial) await api.put(`/gl-accounts/${initial.id}`, body)
      else await api.post('/gl-accounts', body)
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
      title={initial ? '会計科目の編集' : '会計科目の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="gl-account-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="gl-account-form" onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-3 gap-3">
          <Field label="コード" required error={fieldError(error, 'code')} hint="会計システムのコード">
            {(p) => <Input {...p} autoFocus value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" />}
          </Field>
          <div className="col-span-2">
            <Field label="名前" required error={fieldError(error, 'name')}>
              {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 減価償却費" />}
            </Field>
          </div>
        </div>
        <label className="flex items-center gap-2 text-sm text-slate-700">
          <input type="checkbox" className="size-4 rounded border-slate-300" checked={excluded} onChange={(e) => setExcluded(e.target.checked)} />
          対象外（P/L に関係ない会計科目。取込で読み飛ばします）
        </label>
        {!excluded && (
          <Field label="アプリの科目" required error={fieldError(error, 'subject_id')} hint="複数の会計科目を1つの科目にまとめられます">
            {(p) => (
              <Select {...p} value={subjectId} onChange={(e) => setSubjectId(e.target.value)}>
                <option value="">選択してください</option>
                {subjects.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.code} {s.name}（{categoryLabels[s.category]}）
                  </option>
                ))}
              </Select>
            )}
          </Field>
        )}
        <label className="flex items-start gap-2 text-sm text-slate-700">
          <input type="checkbox" className="mt-0.5 size-4 rounded border-slate-300" checked={hide} onChange={(e) => setHide(e.target.checked)} />
          <span>
            明細を FP&A 以外に見せない
            <span className="block text-xs text-slate-500">給与など、摘要に個人名が入りやすい会計科目に使います。FP&A 以外には金額の合計だけを表示します。</span>
          </span>
        </label>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['code', 'name', 'subject_id', 'reason']} />
      </form>
    </Dialog>
  )
}
