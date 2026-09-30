import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { FunctionItem, List, TreeNode, User } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { buildTree, isLeaf, pathName, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

/** 機能（課・チームなど、施策を束ねる単位）の管理画面 */
export function FunctionsPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const functions = useApi<List<FunctionItem>>('/functions')
  const segments = useApi<List<TreeNode>>('/segments')
  const organizations = useApi<List<TreeNode>>('/organizations')
  const users = useApi<List<User>>('/users')

  const segTree = useMemo(() => buildTree(segments.data?.items ?? []), [segments.data])
  const orgTree = useMemo(() => buildTree(organizations.data?.items ?? []), [organizations.data])
  const userName = useMemo(() => new Map((users.data?.items ?? []).map((u) => [u.id, u.name])), [users.data])

  const [editing, setEditing] = useState<FunctionItem | 'new' | null>(null)
  const [deleting, setDeleting] = useState<FunctionItem | null>(null)
  const error = functions.error ?? segments.error ?? organizations.error ?? users.error
  const ready = functions.data && segments.data && organizations.data && users.data

  return (
    <>
      <PageHeader
        title="機能"
        description="施策を束ねる単位（課・グループ・チームなど）です。末端のセグメントと末端の組織に1つずつ所属します。担当者は、配下の施策を作成・編集できるマネージャーです。"
        actions={canWrite && <Button variant="primary" onClick={() => setEditing('new')}>＋ 機能を追加</Button>}
      />
      <Card>
        {error ? (
          <ErrorMessage error={error} />
        ) : !ready ? (
          <Loading />
        ) : functions.data!.items.length === 0 ? (
          <Empty>機能が登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>名称</th>
                <th>セグメント</th>
                <th>組織</th>
                <th>担当者</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {functions.data!.items.map((f) => (
                <tr key={f.id}>
                  <td className="font-medium">{f.name}</td>
                  <td className="text-slate-600">{pathName(segTree, f.segment_id)}</td>
                  <td className="text-slate-600">{pathName(orgTree, f.organization_id)}</td>
                  <td>{f.owner_user_id ? userName.get(f.owner_user_id) : <span className="text-slate-400">未設定</span>}</td>
                  {canWrite && (
                    <td className="text-right">
                      <Button size="sm" variant="ghost" onClick={() => setEditing(f)}>
                        編集
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => setDeleting(f)}>
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

      {editing && ready && (
        <FunctionDialog
          initial={editing === 'new' ? null : editing}
          segTree={segTree}
          orgTree={orgTree}
          users={users.data!.items}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            functions.reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="機能の削除"
        message={<>「{deleting?.name}」を削除します。施策が所属している場合は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/functions/${deleting!.id}`, { reason })
          await functions.reload()
        }}
      />
    </>
  )
}

function FunctionDialog({
  initial,
  segTree,
  orgTree,
  users,
  onClose,
  onSaved,
}: {
  initial: FunctionItem | null
  segTree: Tree
  orgTree: Tree
  users: User[]
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [segmentId, setSegmentId] = useState(initial ? String(initial.segment_id) : '')
  const [organizationId, setOrganizationId] = useState(initial ? String(initial.organization_id) : '')
  const [ownerId, setOwnerId] = useState(initial?.owner_user_id ? String(initial.owner_user_id) : '')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = {
      name,
      segment_id: Number(segmentId) || 0,
      organization_id: Number(organizationId) || 0,
      owner_user_id: ownerId ? Number(ownerId) : null,
      reason,
    }
    try {
      if (initial) await api.put(`/functions/${initial.id}`, body)
      else await api.post('/functions', body)
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
      title={initial ? '機能の編集' : '機能の追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="function-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="function-form" onSubmit={submit} className="space-y-4">
        <Field label="名称" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <LeafSelect label="セグメント" tree={segTree} value={segmentId} onChange={setSegmentId} error={fieldError(error, 'segment_id')} />
        <LeafSelect label="組織" tree={orgTree} value={organizationId} onChange={setOrganizationId} error={fieldError(error, 'organization_id')} />
        <Field label="担当者（マネージャー）" error={fieldError(error, 'owner_user_id')} hint="担当者は、この機能の配下の施策を作成・編集できます（ロールがマネージャーの場合）">
          {(p) => (
            <Select {...p} value={ownerId} onChange={(e) => setOwnerId(e.target.value)}>
              <option value="">（未設定）</option>
              {users
                .filter((u) => u.is_active)
                .map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}（{u.email}）
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['name', 'segment_id', 'organization_id', 'owner_user_id', 'reason']} />
      </form>
    </Dialog>
  )
}

/** 末端ノードだけを選べるセレクト。途中の階層は見出しとして表示する */
function LeafSelect({ label, tree, value, onChange, error }: { label: string; tree: Tree; value: string; onChange: (v: string) => void; error?: string }) {
  return (
    <Field label={label} required error={error} hint="末端の階層のみ選べます">
      {(p) => (
        <Select {...p} value={value} onChange={(e) => onChange(e.target.value)}>
          <option value="">選択してください</option>
          {tree.ordered.map(({ node, depth }) => (
            <option key={node.id} value={node.id} disabled={!isLeaf(tree, node.id)}>
              {'　'.repeat(depth)}
              {node.name}
            </option>
          ))}
        </Select>
      )}
    </Field>
  )
}
