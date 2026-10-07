import { CsvActions } from '../../components/CsvTransfer'
import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { unitTypeLabels, type Unit, type List, type TreeNode, type UnitType, type User } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { buildTree, isLeaf, pathName, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

const unitTypeTone: Record<UnitType, 'indigo' | 'amber' | 'slate'> = {
  service: 'indigo',
  cost_center: 'amber',
  corporate: 'slate',
}

/** ユニット（施策を束ねる単位）の管理画面 */
export function UnitsPage() {
  const user = useCurrentUser()
  const canWrite = user.role === 'fpa_admin'
  const [showArchived, setShowArchived] = useState(false)
  const units = useApi<List<Unit>>(showArchived ? '/units?include_archived=true' : '/units')
  const segments = useApi<List<TreeNode>>('/segments')
  const organizations = useApi<List<TreeNode>>('/organizations')
  const users = useApi<List<User>>('/users')

  const segTree = useMemo(() => buildTree(segments.data?.items ?? []), [segments.data])
  const orgTree = useMemo(() => buildTree(organizations.data?.items ?? []), [organizations.data])
  const userName = useMemo(() => new Map((users.data?.items ?? []).map((u) => [u.id, u.name])), [users.data])

  const [editing, setEditing] = useState<Unit | 'new' | null>(null)
  const [deleting, setDeleting] = useState<Unit | null>(null)
  const [merging, setMerging] = useState<Unit | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)
  const setArchived = async (f: Unit, archived: boolean) => {
    setActionError(null)
    try {
      await api.post(`/units/${f.id}/${archived ? 'archive' : 'unarchive'}`, {})
      await units.reload()
    } catch (err) {
      setActionError(err)
    }
  }
  const error = units.error ?? segments.error ?? organizations.error ?? users.error
  const ready = units.data && segments.data && organizations.data && users.data

  return (
    <>
      <PageHeader
        title="ユニット"
        description="施策を束ねる単位です。サービスのほか、共通経費や管理部門の箱もユニットとして登録し、種別で区別します。末端のセグメントと末端の組織に1つずつ所属します。担当者は、配下の施策を作成・編集できるマネージャーです。"
        actions={
          <>
            <CsvActions
              resource="units"
              label="ユニット"
              canImport={canWrite}
              columns="code,name,unit_type,segment_code,organization_code,owner_email"
              notes={<p>unit_type は service / cost_center / corporate。セグメント・組織は末端のもののコード、担当者はメールアドレスで指定します。</p>}
              onImported={() => units.reload()}
            />
            {canWrite && (
              <Button variant="primary" onClick={() => setEditing('new')}>
                ＋ ユニットを追加
              </Button>
            )}
          </>
        }
      />
      <div className="mb-2 flex items-center justify-between gap-2">
        <label className="flex items-center gap-2 text-sm text-slate-600">
          <input type="checkbox" className="size-4 rounded border-slate-300" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
          廃止したユニットも表示
        </label>
      </div>
      {actionError ? (
        <div className="mb-2">
          <ErrorMessage error={actionError} />
        </div>
      ) : null}
      <Card>
        {error ? (
          <ErrorMessage error={error} />
        ) : !ready ? (
          <Loading />
        ) : units.data!.items.length === 0 ? (
          <Empty>ユニットが登録されていません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-28">コード</th>
                <th>名称</th>
                <th>種別</th>
                <th>セグメント</th>
                <th>組織</th>
                <th>担当者</th>
                {canWrite && <th className="w-32" />}
              </tr>
            </thead>
            <tbody>
              {units.data!.items.map((f) => (
                <tr key={f.id} className={f.is_archived ? 'text-slate-400' : undefined}>
                  <td className="font-mono text-xs">{f.code}</td>
                  <td className="font-medium">
                    {f.name}
                    {f.is_archived && (
                      <span className="ml-1">
                        <Badge>廃止</Badge>
                      </span>
                    )}
                  </td>
                  <td>
                    <Badge tone={unitTypeTone[f.unit_type]}>{unitTypeLabels[f.unit_type]}</Badge>
                  </td>
                  <td className="text-slate-600">{pathName(segTree, f.segment_id)}</td>
                  <td className="text-slate-600">{pathName(orgTree, f.organization_id)}</td>
                  <td>{f.owner_user_id ? userName.get(f.owner_user_id) : <span className="text-slate-400">未設定</span>}</td>
                  {canWrite && (
                    <td className="text-right whitespace-nowrap">
                      {f.is_archived ? (
                        <Button size="sm" variant="ghost" onClick={() => setArchived(f, false)}>
                          廃止を取り消す
                        </Button>
                      ) : (
                        <>
                          <Button size="sm" variant="ghost" onClick={() => setEditing(f)}>
                            編集
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setMerging(f)} aria-label={`${f.name}を統合`}>
                            統合
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setArchived(f, true)} aria-label={`${f.name}を廃止`}>
                            廃止
                          </Button>
                        </>
                      )}
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

      {merging && units.data && (
        <MergeDialog
          unit={merging}
          units={units.data.items.filter((u) => !u.is_archived && u.id !== merging.id)}
          onClose={() => setMerging(null)}
          onMerged={async () => {
            setMerging(null)
            await units.reload()
          }}
        />
      )}
      {editing && ready && (
        <UnitDialog
          initial={editing === 'new' ? null : editing}
          segTree={segTree}
          orgTree={orgTree}
          users={users.data!.items}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            units.reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        title="ユニットの削除"
        message={<>「{deleting?.name}」を削除します。施策が所属している場合は削除できません。</>}
        reason="optional"
        onClose={() => setDeleting(null)}
        onConfirm={async (reason) => {
          await api.del(`/units/${deleting!.id}`, { reason })
          await units.reload()
        }}
      />
    </>
  )
}

function UnitDialog({
  initial,
  segTree,
  orgTree,
  users,
  onClose,
  onSaved,
}: {
  initial: Unit | null
  segTree: Tree
  orgTree: Tree
  users: User[]
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [code, setCode] = useState(initial?.code ?? '')
  const [unitType, setUnitType] = useState<UnitType>(initial?.unit_type ?? 'service')
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
      code,
      name,
      unit_type: unitType,
      segment_id: Number(segmentId) || 0,
      organization_id: Number(organizationId) || 0,
      owner_user_id: ownerId ? Number(ownerId) : null,
      reason,
    }
    try {
      if (initial) await api.put(`/units/${initial.id}`, body)
      else await api.post('/units', body)
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
      title={initial ? 'ユニットの編集' : 'ユニットの追加'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="unit-form" disabled={busy}>
            {busy ? '保存中…' : '保存'}
          </Button>
        </>
      }
    >
      <form id="unit-form" onSubmit={submit} className="space-y-4">
        <Field label="コード" error={fieldError(error, 'code')} hint={initial ? 'CSV の取込で使います（英数字・-・_）' : '空欄なら自動で採番します（UNIT-0001 形式）'}>
          {(p) => <Input {...p} value={code} onChange={(e) => setCode(e.target.value)} className="font-mono" placeholder={initial ? undefined : '自動採番'} />}
        </Field>
        <Field label="名称" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="例: SaaS Aサービス、受託事業共通経費、人事部" />}
        </Field>
        <Field label="種別" required error={fieldError(error, 'unit_type')} hint="サービス = 収益を生むユニット、共通費 = 事業の共通経費、管理部門 = 人事・経理など">
          {(p) => (
            <Select {...p} value={unitType} onChange={(e) => setUnitType(e.target.value as UnitType)}>
              {Object.entries(unitTypeLabels).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <LeafSelect label="セグメント" tree={segTree} value={segmentId} onChange={setSegmentId} error={fieldError(error, 'segment_id')} />
        <LeafSelect label="組織" tree={orgTree} value={organizationId} onChange={setOrganizationId} error={fieldError(error, 'organization_id')} />
        <Field label="担当者（マネージャー）" error={fieldError(error, 'owner_user_id')} hint="担当者は、このユニットの配下の施策を作成・編集できます（ロールがマネージャーの場合）">
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
        <FormError error={error} fields={['code', 'name', 'unit_type', 'segment_id', 'organization_id', 'owner_user_id', 'reason']} />
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

/** ユニットの統合（docs/plan.md「2.15」）。施策をすべて統合先へ移し、元のユニットを廃止にする */
function MergeDialog({ unit, units, onClose, onMerged }: { unit: Unit; units: Unit[]; onClose: () => void; onMerged: () => Promise<void> }) {
  const [target, setTarget] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.post(`/units/${unit.id}/merge`, { target_unit_id: Number(target) || 0, reason })
      await onMerged()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      title="ユニットの統合"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="merge-form" disabled={busy || !target || !reason.trim()}>
            {busy ? '処理中…' : '統合する'}
          </Button>
        </>
      }
    >
      <form id="merge-form" onSubmit={submit} className="space-y-4">
        <p className="text-sm text-slate-600">
          「{unit.name}」の施策をすべて統合先へ移し、「{unit.name}」を廃止にします。過去のシナリオも含め、移した施策の数字は統合先で集計されます。日付を決めて行うときは、組織変更の予約を使ってください。
        </p>
        <Field label="統合先のユニット" required error={fieldError(error, 'target_unit_id')}>
          {(p) => (
            <Select {...p} value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="">選択してください</option>
              {units.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.name}（{u.code}）
                </option>
              ))}
            </Select>
          )}
        </Field>
        <Field label="変更理由" required error={fieldError(error, 'reason')}>
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" placeholder="例: 10月の組織改編で課を統合" />}
        </Field>
        <FormError error={error} fields={['target_unit_id', 'reason']} />
      </form>
    </Dialog>
  )
}
