import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import {
  orgChangeKindLabels,
  orgChangeStatusLabels,
  type Activity,
  type List,
  type OrgChangeItem,
  type OrgChangeKind,
  type OrgChangePlan,
  type OrgChangeStatus,
  type TreeNode,
  type Unit,
  type User,
} from '../../api/types'
import { useReason } from '../../components/ReasonDialog'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatDateTime } from '../../lib/format'
import { todayInTokyo } from '../../lib/scenario'
import { buildTree, isLeaf, pathName, type Tree } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

const statusTone: Record<OrgChangeStatus, 'indigo' | 'green' | 'red' | 'slate'> = {
  scheduled: 'indigo',
  applied: 'green',
  failed: 'red',
  cancelled: 'slate',
}

/** 画面の表示に使うマスタ */
type Masters = {
  activities: Activity[]
  units: Unit[]
  segments: Tree
  organizations: Tree
  users: User[]
}

/**
 * 組織変更の予約（FP&A のみ、docs/plan.md「2.15」）。施策の移動・ユニットの所属の変更・統合・担当者の変更をまとめて登録し、
 * 有効日の 0:00 に自動で適用する。全部か無しかで、失敗したら理由を表示する。
 */
export function OrgChangesPage() {
  const me = useCurrentUser()
  if (me.role !== 'fpa_admin') {
    return <PageHeader title="組織変更の予約" description="この画面は FP&A のみが使えます。" />
  }
  return <OrgChangesView />
}

function OrgChangesView() {
  const plans = useApi<List<OrgChangePlan>>('/org-change-plans')
  const activities = useApi<List<Activity>>('/activities')
  const units = useApi<List<Unit>>('/units?include_archived=true')
  const segments = useApi<List<TreeNode>>('/segments')
  const organizations = useApi<List<TreeNode>>('/organizations')
  const users = useApi<List<User>>('/users')
  const segTree = useMemo(() => buildTree(segments.data?.items ?? []), [segments.data])
  const orgTree = useMemo(() => buildTree(organizations.data?.items ?? []), [organizations.data])
  const [editing, setEditing] = useState<OrgChangePlan | 'new' | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)
  const { askReason, dialog: reasonDialog } = useReason()

  const error = plans.error ?? activities.error ?? units.error ?? segments.error ?? organizations.error ?? users.error
  const masters: Masters | null =
    activities.data && units.data && segments.data && organizations.data && users.data
      ? { activities: activities.data.items, units: units.data.items, segments: segTree, organizations: orgTree, users: users.data.items }
      : null

  const refresh = () => Promise.all([plans.reload(), activities.reload(), units.reload()])
  const run = async (op: () => Promise<unknown>) => {
    setActionError(null)
    try {
      await op()
      await refresh()
    } catch (err) {
      setActionError(err)
      await plans.reload()
    }
  }
  const open = async (p: OrgChangePlan) => setEditing(await api.get<OrgChangePlan>(`/org-change-plans/${p.id}`))

  return (
    <>
      <PageHeader
        title="組織変更の予約"
        description="施策の移動・ユニットの所属の変更・ユニットの統合・担当者の変更をまとめて登録し、有効日の 0:00 に自動で適用します。1つでも適用できない変更があれば何も変えず、失敗として理由を表示します。過去のシナリオも含め、数字は常に今の組織で集計されます。"
        actions={
          <Button variant="primary" onClick={() => setEditing('new')} disabled={!masters}>
            ＋ 予約を作成
          </Button>
        }
      />
      {actionError ? (
        <div className="mb-4">
          <ErrorMessage error={actionError} />
        </div>
      ) : null}
      <Card>
        {error ? (
          <ErrorMessage error={error} />
        ) : !plans.data ? (
          <Loading />
        ) : plans.data.items.length === 0 ? (
          <Empty>組織変更の予約はありません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>名前</th>
                <th>有効日</th>
                <th>状態</th>
                <th className="text-right">変更</th>
                <th>作成</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {plans.data.items.map((p) => (
                <tr key={p.id}>
                  <td className="font-medium">
                    {p.name}
                    {p.status === 'failed' && p.error && <div className="mt-0.5 text-xs font-normal text-red-700">{p.error}</div>}
                  </td>
                  <td className="whitespace-nowrap tabular-nums">{p.effective_date}</td>
                  <td className="whitespace-nowrap">
                    <Badge tone={statusTone[p.status]}>{orgChangeStatusLabels[p.status]}</Badge>
                    {p.applied_at && <div className="text-xs text-slate-500">{formatDateTime(p.applied_at)}</div>}
                  </td>
                  <td className="text-right tabular-nums">{p.item_count} 件</td>
                  <td className="text-xs text-slate-500">{p.created_by_name}</td>
                  <td className="text-right whitespace-nowrap">
                    <Button size="sm" variant="ghost" onClick={() => open(p)} disabled={!masters} aria-label={`${p.name}を開く`}>
                      {p.status === 'scheduled' || p.status === 'failed' ? '編集' : '内容'}
                    </Button>
                    {(p.status === 'scheduled' || p.status === 'failed') && (
                      <>
                        <Button size="sm" variant="ghost" onClick={() => run(() => api.post(`/org-change-plans/${p.id}/apply`, {}))} aria-label={`${p.name}を今すぐ適用`}>
                          今すぐ適用
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => run(() => api.post(`/org-change-plans/${p.id}/cancel`, {}))}>
                          取り消し
                        </Button>
                      </>
                    )}
                    {p.status !== 'applied' && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          run(() =>
                            askReason(`「${p.name}」を削除する理由`, async (reason) => {
                              await api.del(`/org-change-plans/${p.id}`, { reason })
                            }),
                          )
                        }
                      >
                        削除
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {editing && masters && (
        <PlanDialog
          initial={editing === 'new' ? null : editing}
          masters={masters}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null)
            await refresh()
          }}
        />
      )}
      {reasonDialog}
    </>
  )
}

const emptyItem = (kind: OrgChangeKind): OrgChangeItem => ({
  kind,
  activity_id: null,
  unit_id: null,
  target_unit_id: null,
  segment_id: null,
  organization_id: null,
  owner_user_id: null,
})

/** 変更の説明（例: 施策 A（ACT-1）を CCC課 へ） */
function describe(it: OrgChangeItem, m: Masters): string {
  const act = (id: number | null) => {
    const a = m.activities.find((x) => x.id === id)
    return a ? `${a.name}（${a.code}）` : `施策 #${id}`
  }
  const unit = (id: number | null) => m.units.find((x) => x.id === id)?.name ?? `ユニット #${id}`
  const user = (id: number | null) => (id === null ? '未設定' : (m.users.find((x) => x.id === id)?.name ?? `ユーザー #${id}`))
  switch (it.kind) {
    case 'move_activity':
      return `施策 ${act(it.activity_id)} を「${unit(it.target_unit_id)}」へ移す`
    case 'move_unit':
      return `ユニット「${unit(it.unit_id)}」の所属を「${pathName(m.segments, it.segment_id)}」／「${pathName(m.organizations, it.organization_id)}」にする`
    case 'merge_unit':
      return `ユニット「${unit(it.unit_id)}」を「${unit(it.target_unit_id)}」に統合する`
    case 'change_owner':
      return it.activity_id !== null ? `施策 ${act(it.activity_id)} の担当者を ${user(it.owner_user_id)} にする` : `ユニット「${unit(it.unit_id)}」のマネージャーを ${user(it.owner_user_id)} にする`
  }
}

function tomorrow(): string {
  const d = new Date(`${todayInTokyo()}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + 1)
  return d.toISOString().slice(0, 10)
}

function PlanDialog({ initial, masters, onClose, onSaved }: { initial: OrgChangePlan | null; masters: Masters; onClose: () => void; onSaved: () => Promise<void> }) {
  const editable = initial === null || initial.status === 'scheduled' || initial.status === 'failed'
  const [name, setName] = useState(initial?.name ?? '')
  const [date, setDate] = useState(initial?.effective_date ?? tomorrow())
  const [items, setItems] = useState<OrgChangeItem[]>(initial?.items ?? [])
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    const body = { name, effective_date: date, items, reason }
    try {
      if (initial) await api.put(`/org-change-plans/${initial.id}`, body)
      else await api.post('/org-change-plans', body)
      await onSaved()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      wide
      title={initial ? (editable ? '組織変更の予約の編集' : '組織変更の予約') : '組織変更の予約の作成'}
      onClose={onClose}
      footer={
        editable ? (
          <>
            <Button onClick={onClose}>キャンセル</Button>
            <Button variant="primary" type="submit" form="plan-form" disabled={busy || items.length === 0}>
              {busy ? '保存中…' : '保存'}
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={onClose}>
            閉じる
          </Button>
        )
      }
    >
      <form id="plan-form" onSubmit={submit} className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="sm:col-span-2">
            <Field label="名前" required error={fieldError(error, 'name')}>
              {(p) => <Input {...p} value={name} disabled={!editable} onChange={(e) => setName(e.target.value)} placeholder="例: 2026年10月の組織改編" />}
            </Field>
          </div>
          <Field label="有効日" required error={fieldError(error, 'effective_date')} hint="この日の 0:00 に適用します">
            {(p) => <Input {...p} type="date" min={tomorrow()} value={date} disabled={!editable} onChange={(e) => setDate(e.target.value)} />}
          </Field>
        </div>

        <div>
          <h3 className="mb-1 text-sm font-medium text-slate-700">変更（上から順に適用します）</h3>
          {items.length === 0 ? (
            <p className="text-sm text-slate-500">変更はまだありません。下の「変更を追加」から登録してください。</p>
          ) : (
            <ol className="space-y-1 text-sm" aria-label="変更の一覧">
              {items.map((it, i) => (
                <li key={i} className="flex items-start justify-between gap-2 rounded border border-slate-200 px-2 py-1">
                  <span>
                    <span className="mr-1 text-xs text-slate-400 tabular-nums">{i + 1}.</span>
                    <Badge tone="slate">{orgChangeKindLabels[it.kind]}</Badge> {describe(it, masters)}
                  </span>
                  {editable && (
                    <Button size="sm" variant="ghost" onClick={() => setItems(items.filter((_, j) => j !== i))} aria-label={`${i + 1} 件目の変更を削除`}>
                      削除
                    </Button>
                  )}
                </li>
              ))}
            </ol>
          )}
          {fieldError(error, 'items') && <p className="mt-1 text-xs text-red-600">{fieldError(error, 'items')}</p>}
        </div>

        {editable && <ItemForm masters={masters} onAdd={(added) => setItems([...items, ...added])} />}

        {editable && (
          <Field label="変更理由（任意）" error={fieldError(error, 'reason')}>
            {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
          </Field>
        )}
        <FormError error={error} fields={['name', 'effective_date', 'items', 'reason']} />
      </form>
    </Dialog>
  )
}

/** 変更を追加するフォーム。施策の移動は、複数の施策をまとめて選べる */
function ItemForm({ masters, onAdd }: { masters: Masters; onAdd: (items: OrgChangeItem[]) => void }) {
  const [kind, setKind] = useState<OrgChangeKind>('move_activity')
  const [draft, setDraft] = useState<OrgChangeItem>(emptyItem('move_activity'))
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [filter, setFilter] = useState('')
  const [ownerTarget, setOwnerTarget] = useState<'activity' | 'unit'>('activity')
  const activeUnits = masters.units.filter((u) => !u.is_archived)
  const activeUsers = masters.users.filter((u) => u.is_active)
  const num = (v: string) => (v ? Number(v) : null)
  const set = (patch: Partial<OrgChangeItem>) => setDraft({ ...draft, ...patch })

  const reset = (k: OrgChangeKind) => {
    setKind(k)
    setDraft(emptyItem(k))
    setPicked(new Set())
  }
  const ready =
    kind === 'move_activity'
      ? draft.target_unit_id !== null && picked.size > 0
      : kind === 'move_unit'
        ? draft.unit_id !== null && draft.segment_id !== null && draft.organization_id !== null
        : kind === 'merge_unit'
          ? draft.unit_id !== null && draft.target_unit_id !== null
          : ownerTarget === 'activity'
            ? draft.activity_id !== null
            : draft.unit_id !== null
  const add = () => {
    if (kind === 'move_activity') {
      onAdd([...picked].map((id) => ({ ...emptyItem('move_activity'), activity_id: id, target_unit_id: draft.target_unit_id })))
    } else if (kind === 'change_owner') {
      onAdd([{ ...draft, activity_id: ownerTarget === 'activity' ? draft.activity_id : null, unit_id: ownerTarget === 'unit' ? draft.unit_id : null }])
    } else {
      onAdd([draft])
    }
    reset(kind)
  }

  const unitSelect = (label: string, value: number | null, onChange: (v: number | null) => void, exclude?: number | null) => (
    <Field label={label}>
      {(p) => (
        <Select {...p} value={value ?? ''} onChange={(e) => onChange(num(e.target.value))}>
          <option value="">選択してください</option>
          {activeUnits
            .filter((u) => u.id !== exclude)
            .map((u) => (
              <option key={u.id} value={u.id}>
                {u.name}（{u.code}）
              </option>
            ))}
        </Select>
      )}
    </Field>
  )
  const leafSelect = (label: string, tree: Tree, value: number | null, onChange: (v: number | null) => void) => (
    <Field label={label} hint="末端のノードのみ">
      {(p) => (
        <Select {...p} value={value ?? ''} onChange={(e) => onChange(num(e.target.value))}>
          <option value="">選択してください</option>
          {tree.ordered
            .map((o) => o.node)
            .filter((n) => isLeaf(tree, n.id))
            .map((n) => (
              <option key={n.id} value={n.id}>
                {pathName(tree, n.id)}
              </option>
            ))}
        </Select>
      )}
    </Field>
  )
  const shownActivities = masters.activities.filter((a) => !filter || a.name.includes(filter) || a.code.includes(filter))

  return (
    <fieldset className="space-y-3 rounded-md border border-slate-200 p-3" aria-label="変更を追加">
      <legend className="px-1 text-sm font-medium text-slate-700">変更を追加</legend>
      <Field label="変更の種類">
        {(p) => (
          <Select {...p} value={kind} onChange={(e) => reset(e.target.value as OrgChangeKind)} className="w-56">
            {(Object.keys(orgChangeKindLabels) as OrgChangeKind[]).map((k) => (
              <option key={k} value={k}>
                {orgChangeKindLabels[k]}
              </option>
            ))}
          </Select>
        )}
      </Field>

      {kind === 'move_activity' && (
        <div className="space-y-2">
          {unitSelect('移動先のユニット', draft.target_unit_id, (v) => set({ target_unit_id: v }))}
          <Input aria-label="施策を絞り込む" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="施策の名前・コードで絞り込む" className="w-64" />
          <ul className="max-h-48 space-y-0.5 overflow-y-auto rounded border border-slate-100 p-1 text-sm" aria-label="移す施策">
            {shownActivities.map((a) => (
              <li key={a.id}>
                <label className="flex items-center gap-2 px-1">
                  <input
                    type="checkbox"
                    className="size-4 rounded border-slate-300"
                    checked={picked.has(a.id)}
                    onChange={(e) => {
                      const next = new Set(picked)
                      if (e.target.checked) next.add(a.id)
                      else next.delete(a.id)
                      setPicked(next)
                    }}
                  />
                  {a.name}
                  <span className="font-mono text-xs text-slate-400">{a.code}</span>
                  <span className="text-xs text-slate-400">（{masters.units.find((u) => u.id === a.unit_id)?.name}）</span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}
      {kind === 'move_unit' && (
        <div className="grid gap-3 sm:grid-cols-3">
          {unitSelect('ユニット', draft.unit_id, (v) => set({ unit_id: v }))}
          {leafSelect('所属先のセグメント', masters.segments, draft.segment_id, (v) => set({ segment_id: v }))}
          {leafSelect('所属先の組織', masters.organizations, draft.organization_id, (v) => set({ organization_id: v }))}
        </div>
      )}
      {kind === 'merge_unit' && (
        <div className="grid gap-3 sm:grid-cols-2">
          {unitSelect('統合するユニット（廃止になる）', draft.unit_id, (v) => set({ unit_id: v }))}
          {unitSelect('統合先のユニット', draft.target_unit_id, (v) => set({ target_unit_id: v }), draft.unit_id)}
        </div>
      )}
      {kind === 'change_owner' && (
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="対象">
            {(p) => (
              <Select {...p} value={ownerTarget} onChange={(e) => setOwnerTarget(e.target.value as 'activity' | 'unit')}>
                <option value="activity">施策の担当者</option>
                <option value="unit">ユニットのマネージャー</option>
              </Select>
            )}
          </Field>
          {ownerTarget === 'activity' ? (
            <Field label="施策">
              {(p) => (
                <Select {...p} value={draft.activity_id ?? ''} onChange={(e) => set({ activity_id: num(e.target.value) })}>
                  <option value="">選択してください</option>
                  {masters.activities.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          ) : (
            unitSelect('ユニット', draft.unit_id, (v) => set({ unit_id: v }))
          )}
          <Field label="変更後">
            {(p) => (
              <Select {...p} value={draft.owner_user_id ?? ''} onChange={(e) => set({ owner_user_id: num(e.target.value) })}>
                <option value="">未設定</option>
                {activeUsers.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.name}
                  </option>
                ))}
              </Select>
            )}
          </Field>
        </div>
      )}
      <Button onClick={add} disabled={!ready}>
        {kind === 'move_activity' && picked.size > 1 ? `${picked.size} 件の変更を追加` : '変更を追加'}
      </Button>
    </fieldset>
  )
}
