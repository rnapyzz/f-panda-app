import { CsvActions } from '../../components/CsvTransfer'
import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { categoryLabels, type List, type Subject, type SubjectCategory } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { useApi } from '../../lib/useApi'

/** 勘定科目の管理画面 */
export function SubjectsPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const { data, error, loading, reload } = useApi<List<Subject>>('/subjects')
  const subjects = useMemo(() => data?.items ?? [], [data])
  const byId = useMemo(() => new Map(subjects.map((s) => [s.id, s])), [subjects])
  const [editing, setEditing] = useState<Subject | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Subject | null>(null)

  return (
    <>
      <PageHeader
        title="勘定科目"
        description="P/L を構成する科目です。科目コードは実績 CSV の取込で使います。"
        actions={
          <>
            <CsvActions
              resource="subjects"
              label="勘定科目"
              canImport={canWrite}
              columns="code,name,category,parent_code,sort_order,is_restricted"
              notes={
                <>
                  <p>category は revenue（収益）/ expense（費用）。親科目は同じ区分のもののコードを指定します。</p>
                  <p>is_restricted は閲覧制限（true / false）。列を省略すると、今の設定を変えません。</p>
                </>
              }
              onImported={reload}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ 科目を追加
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
        ) : subjects.length === 0 ? (
          <Empty>科目が登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-28">コード</th>
                <th>科目名</th>
                <th className="w-20">区分</th>
                <th>親科目</th>
                <th className="w-20 text-right">表示順</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {subjects.map((s) => (
                <tr key={s.id}>
                  <td className="font-mono text-xs">{s.code}</td>
                  <td className="font-medium">
                    {s.name}
                    {s.is_restricted && (
                      <span className="ml-2">
                        <Badge tone="red">閲覧制限</Badge>
                      </span>
                    )}
                  </td>
                  <td>
                    <Badge tone={s.category === 'revenue' ? 'green' : 'amber'}>{categoryLabels[s.category]}</Badge>
                  </td>
                  <td className="text-slate-600">{s.parent_id ? byId.get(s.parent_id)?.name : ''}</td>
                  <td className="text-right tabular-nums">{s.sort_order}</td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(s)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(s)}>
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

      {editing && (
        <SubjectDialog
          initial={editing === 'new' ? null : editing}
          subjects={subjects}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="科目の削除"
        message={<>「{deleting?.code} {deleting?.name}」を削除します。子科目や金額データがある場合は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/subjects/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

function SubjectDialog({ initial, subjects, onClose, onSaved }: { initial: Subject | null; subjects: Subject[]; onClose: () => void; onSaved: () => void }) {
  const [code, setCode] = useState(initial?.code ?? '')
  const [name, setName] = useState(initial?.name ?? '')
  const [category, setCategory] = useState<SubjectCategory>(initial?.category ?? 'revenue')
  const [parentId, setParentId] = useState(initial?.parent_id ? String(initial.parent_id) : '')
  const [sortOrder, setSortOrder] = useState(String(initial?.sort_order ?? 0))
  const [restricted, setRestricted] = useState(initial?.is_restricted ?? false)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { code, name, category, parent_id: parentId ? Number(parentId) : null, sort_order: Number(sortOrder) || 0, is_restricted: restricted, reason }
    try {
      if (initial) await api.put(`/subjects/${initial.id}`, body)
      else await api.post('/subjects', body)
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
      title={initial ? '科目の編集' : '科目の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="subject-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="subject-form" onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-3 gap-3">
          <Field label="科目コード" required error={fieldError(error, 'code')}>
            {(p) => <Input {...p} autoFocus value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" />}
          </Field>
          <Field label="科目名" required error={fieldError(error, 'name')} className="col-span-2">
            {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
        </div>
        <Field label="区分" required error={fieldError(error, 'category')}>
          {(p) => (
            <Select
              {...p}
              value={category}
              onChange={(e) => {
                setCategory(e.target.value as SubjectCategory)
                setParentId('')
              }}
            >
              <option value="revenue">収益</option>
              <option value="expense">費用</option>
            </Select>
          )}
        </Field>
        <Field label="親科目" error={fieldError(error, 'parent_id')} hint="同じ区分の科目のみ選べます">
          {(p) => (
            <Select {...p} value={parentId} onChange={(e) => setParentId(e.target.value)}>
              <option value="">（なし）</option>
              {subjects
                .filter((s) => s.category === category && s.id !== initial?.id)
                .map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.code} {s.name}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="表示順" error={fieldError(error, 'sort_order')}>
          {(p) => <Input {...p} type="number" value={sortOrder} onChange={(e) => setSortOrder(e.target.value)} className="w-32" />}
        </Field>
        <label className="flex items-start gap-2 text-sm text-slate-700">
          <input type="checkbox" className="mt-0.5 size-4 rounded border-slate-300" checked={restricted} onChange={(e) => setRestricted(e.target.checked)} />
          <span>
            閲覧制限（FP&A と経営陣だけが金額を見られる）
            <span className="block text-xs text-slate-500">人件費など、金額から個人の情報が推測できる科目に使います。現場マネージャー・現場担当には、金額を合計からも除いて表示し、入力もできなくします。</span>
          </span>
        </label>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['code', 'name', 'category', 'parent_id', 'sort_order', 'is_restricted', 'reason']} />
      </form>
    </Dialog>
  )
}
