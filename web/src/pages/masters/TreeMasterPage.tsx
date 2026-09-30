import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { List, TreeNode } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { buildTree, subtreeIds, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

type Resource = 'organizations' | 'segments'

const labels: Record<Resource, { title: string; noun: string; description: string }> = {
  organizations: { title: '組織', noun: '組織', description: '本部・部などの組織の階層です。機能（課・チーム）は末端の組織に所属します。' },
  segments: { title: 'セグメント', noun: 'セグメント', description: '事業ポートフォリオの階層です。機能は末端のセグメントに所属します。' },
}

type Editing = { mode: 'create'; parentId: number | null } | { mode: 'edit'; node: TreeNode }

/** 組織・セグメントの管理画面 */
export function TreeMasterPage({ resource }: { resource: Resource }) {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const l = labels[resource]
  const { data, error, loading, reload } = useApi<List<TreeNode>>(`/${resource}`)
  const tree = useMemo(() => buildTree(data?.items ?? []), [data])
  const [editing, setEditing] = useState<Editing | null>(null)
  const [deleting, setDeleting] = useState<TreeNode | null>(null)

  return (
    <>
      <PageHeader
        title={l.title}
        description={l.description}
        actions={canWrite && <Button variant="primary" onClick={() => setEditing({ mode: 'create', parentId: null })}>＋ 最上位に追加</Button>}
      />
      <Card>
        {loading && !data ? (
          <Loading />
        ) : error ? (
          <ErrorMessage error={error} />
        ) : tree.ordered.length === 0 ? (
          <Empty>{l.noun}が登録されていません</Empty>
        ) : (
          <ul className="divide-y divide-slate-100">
            {tree.ordered.map(({ node, depth }) => (
              <li key={node.id} className="group flex items-center gap-2 py-2" style={{ paddingLeft: depth * 24 }}>
                <span className="text-slate-300">{depth > 0 ? '└' : '■'}</span>
                <span className="font-medium text-slate-800">{node.name}</span>
                <span className="text-xs text-slate-400">第{node.level}階層</span>
                {canWrite && (
                  <span className="ml-auto flex gap-1 opacity-60 group-hover:opacity-100">
                    <Button size="sm" variant="ghost" onClick={() => setEditing({ mode: 'create', parentId: node.id })}>
                      ＋ 配下に追加
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setEditing({ mode: 'edit', node })}>
                      編集
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setDeleting(node)}>
                      削除
                    </Button>
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>

      {editing && (
        <TreeNodeDialog
          resource={resource}
          noun={l.noun}
          tree={tree}
          editing={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title={`${l.noun}の削除`}
        message={<>「{deleting?.name}」を削除します。配下の{l.noun}や所属する機能がある場合は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/${resource}/${deleting!.id}`, { reason })
          await reload()
        }}
      />
    </>
  )
}

function TreeNodeDialog({
  resource,
  noun,
  tree,
  editing,
  onClose,
  onSaved,
}: {
  resource: Resource
  noun: string
  tree: Tree
  editing: Editing
  onClose: () => void
  onSaved: () => void
}) {
  const initial = editing.mode === 'edit' ? editing.node : null
  const [name, setName] = useState(initial?.name ?? '')
  const [parentId, setParentId] = useState<number | null>(initial ? initial.parent_id : editing.mode === 'create' ? editing.parentId : null)
  const [sortOrder, setSortOrder] = useState(String(initial?.sort_order ?? 0))
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  // 自分自身と配下は親にできない
  const excluded = initial ? subtreeIds(tree, initial.id) : new Set<number>()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { name, parent_id: parentId, sort_order: Number(sortOrder) || 0, reason }
    try {
      if (initial) await api.put(`/${resource}/${initial.id}`, body)
      else await api.post(`/${resource}`, body)
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
      title={initial ? `${noun}の編集` : `${noun}の追加`}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="tree-node-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="tree-node-form" onSubmit={submit} className="space-y-4">
        <Field label="名称" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <Field label={`親の${noun}`} error={fieldError(error, 'parent_id')} hint="機能が所属している末端の階層の下には追加できません">
          {(p) => (
            <Select {...p} value={parentId ?? ''} onChange={(e) => setParentId(e.target.value ? Number(e.target.value) : null)}>
              <option value="">（最上位）</option>
              {tree.ordered
                .filter(({ node }) => !excluded.has(node.id))
                .map(({ node, depth }) => (
                  <option key={node.id} value={node.id}>
                    {'　'.repeat(depth)}
                    {node.name}
                  </option>
                ))}
            </Select>
          )}
        </Field>
        <Field label="表示順" hint="小さいほど上に表示します" error={fieldError(error, 'sort_order')}>
          {(p) => <Input {...p} type="number" value={sortOrder} onChange={(e) => setSortOrder(e.target.value)} className="w-32" />}
        </Field>
        <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['name', 'parent_id', 'sort_order', 'reason']} />
      </form>
    </Dialog>
  )
}
