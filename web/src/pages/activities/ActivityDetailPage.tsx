import { useMemo, useState, type ReactNode } from 'react'
import { api } from '../../api/client'
import {
  activityStatusLabels,
  activityTypeLabels,
  calcModeLabels,
  categoryLabels,
  driverKindLabels,
  milestoneStatusLabels,
  type ActivityDetail,
  type Driver,
  type Formula,
  type FunctionItem,
  type List,
  type Milestone,
  type Subject,
  type User,
} from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useReason } from '../../components/ReasonDialog'
import { Badge, Button, Card, Empty, ErrorMessage, Loading, PageHeader, Table } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatPercent } from '../../lib/format'
import { Link, navigate } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { ActivityFormDialog } from './ActivityFormDialog'
import { creatableFunctions, statusTone } from './ActivityListPage'
import { DriverDialog, FormulaDialog, MilestoneDialog } from './ActivityDialogs'
import { ExternalCodesCard } from './ExternalCodesCard'

const milestoneTone = { not_started: 'slate', in_progress: 'indigo', completed: 'green', delayed: 'red' } as const

export function ActivityDetailPage({ id }: { id: string }) {
  const me = useCurrentUser()
  const activity = useApi<ActivityDetail>(`/activities/${id}`)
  const functions = useApi<List<FunctionItem>>('/functions')
  const users = useApi<List<User>>('/users')
  const subjects = useApi<List<Subject>>('/subjects')
  const { withReason, askReason, dialog: reasonDialog } = useReason()

  const [editing, setEditing] = useState(false)
  const [milestone, setMilestone] = useState<Milestone | 'new' | null>(null)
  const [driver, setDriver] = useState<Driver | 'new' | null>(null)
  const [formula, setFormula] = useState<Formula | 'new' | null>(null)
  const [deletingDriver, setDeletingDriver] = useState<Driver | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)

  const functionById = useMemo(() => new Map((functions.data?.items ?? []).map((f) => [f.id, f])), [functions.data])
  const userName = useMemo(() => new Map((users.data?.items ?? []).map((u) => [u.id, u.name])), [users.data])
  const subjectById = useMemo(() => new Map((subjects.data?.items ?? []).map((s) => [s.id, s])), [subjects.data])

  const error = activity.error ?? functions.error ?? users.error ?? subjects.error
  if (error) return <ErrorMessage error={error} />
  if (!activity.data || !functions.data || !users.data || !subjects.data) return <Loading />

  const a = activity.data
  const canEdit = a.can_edit
  const creatable = creatableFunctions(me, functions.data.items)
  const canDelete = creatable.some((f) => f.id === a.function_id)
  // 編集ダイアログで選べるユニット: 移動できるユニット＋現在のユニット
  const editableFunctions = creatable.some((f) => f.id === a.function_id) ? creatable : [functionById.get(a.function_id)!, ...creatable]
  const base = `/activities/${a.id}`
  const reload = () => activity.reload()

  /** 失敗したら画面上部にエラーを表示する */
  const run = async (op: () => Promise<unknown>) => {
    setActionError(null)
    try {
      await op()
    } catch (err) {
      setActionError(err)
    }
  }

  const deleteActivity = () =>
    run(() =>
      askReason('施策の削除', async (reason) => {
        await api.del(base, { reason })
        navigate('/activities')
      }),
    )

  const deleteMilestone = (m: Milestone) =>
    run(() =>
      askReason(`マイルストーン「${m.name}」の削除`, async (reason) => {
        await api.del(`${base}/milestones/${m.id}`, { reason })
        await reload()
      }),
    )

  const deleteFormula = (f: Formula) =>
    run(() =>
      askReason(`「${subjectById.get(f.subject_id)?.name}」の計算式の削除`, async (reason) => {
        await api.del(`${base}/formulas/${f.subject_id}`, { reason })
        await reload()
      }),
    )

  return (
    <>
      <div className="mb-2 text-sm">
        <Link to="/activities" className="text-slate-500 hover:text-slate-700">
          ← 施策一覧
        </Link>
      </div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-2">
            {a.name}
            <Badge tone={statusTone[a.status]}>{activityStatusLabels[a.status]}</Badge>
          </span>
        }
        description={<span className="font-mono">{a.code}</span>}
        actions={
          <>
            <Link
              to={`/scenarios?activity_id=${a.id}`}
              className="inline-flex items-center rounded-md border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              数値を見る・入力する
            </Link>
            <Link
              to={`/history?activity_id=${a.id}`}
              className="inline-flex items-center rounded-md border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              変更履歴
            </Link>
            {canEdit && (
              <Button variant="primary" onClick={() => setEditing(true)}>
                編集
              </Button>
            )}
            {canDelete && (
              <Button variant="ghost" onClick={deleteActivity}>
                削除
              </Button>
            )}
          </>
        }
      />
      {actionError ? (
        <div className="mb-4">
          <ErrorMessage error={actionError} />
        </div>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-1">
          <Card title="基本情報">
            <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 text-sm">
              <Info label="タイプ">{activityTypeLabels[a.activity_type]}</Info>
              <Info label="ユニット">{functionById.get(a.function_id)?.name}</Info>
              <Info label="担当者">{a.owner_user_id ? userName.get(a.owner_user_id) : '未設定'}</Info>
              <Info label="期間">{a.start_date || a.end_date ? `${a.start_date ?? ''} 〜 ${a.end_date ?? ''}` : '—'}</Info>
              <Info label="確度">{formatPercent(a.probability)}</Info>
              <Info label="算出方式">{calcModeLabels[a.calc_mode]}</Info>
            </dl>
            <div className="mt-4 border-t border-slate-100 pt-3">
              <h3 className="mb-1 text-xs font-semibold text-slate-500">前提条件</h3>
              <p className="text-sm whitespace-pre-wrap text-slate-700">{a.assumptions || <span className="text-slate-400">未入力</span>}</p>
            </div>
          </Card>
          <ExternalCodesCard activityId={a.id} codes={a.external_codes} canEdit={canEdit} onChanged={reload} />
        </div>

        <div className="space-y-4 lg:col-span-2">
          <Card title="マイルストーン" actions={canEdit && <Button size="sm" onClick={() => setMilestone('new')}>＋ 追加</Button>}>
            {a.milestones.length === 0 ? (
              <Empty>マイルストーンはありません</Empty>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <th className="w-28">期日</th>
                    <th>名称</th>
                    <th className="w-24">状態</th>
                    {canEdit && <th className="w-28" />}
                  </tr>
                </thead>
                <tbody>
                  {a.milestones.map((m) => (
                    <tr key={m.id}>
                      <td className="tabular-nums">{m.due_date}</td>
                      <td>{m.name}</td>
                      <td>
                        <Badge tone={milestoneTone[m.status]}>{milestoneStatusLabels[m.status]}</Badge>
                      </td>
                      {canEdit && (
                        <td className="text-right">
                          <Button size="sm" variant="ghost" onClick={() => setMilestone(m)}>
                            編集
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => deleteMilestone(m)}>
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

          <Card title="ドライバー・KPI" actions={canEdit && <Button size="sm" onClick={() => setDriver('new')}>＋ 追加</Button>}>
            {a.drivers.length === 0 ? (
              <Empty>ドライバーはありません。単価・件数・顧客数など、金額の根拠となる値を登録します。</Empty>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <th>名称</th>
                    <th>コード（計算式で使う名前）</th>
                    <th>種別</th>
                    <th>単位</th>
                    {canEdit && <th className="w-28" />}
                  </tr>
                </thead>
                <tbody>
                  {a.drivers.map((d) => (
                    <tr key={d.id}>
                      <td className="font-medium">{d.name}</td>
                      <td className="font-mono text-xs">{d.code}</td>
                      <td className="text-slate-600">{driverKindLabels[d.driver_kind]}</td>
                      <td className="text-slate-600">{d.unit}</td>
                      {canEdit && (
                        <td className="text-right">
                          <Button size="sm" variant="ghost" onClick={() => setDriver(d)}>
                            編集
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setDeletingDriver(d)}>
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

          <Card title="計算式" actions={canEdit && <Button size="sm" onClick={() => setFormula('new')}>＋ 追加</Button>}>
            {a.calc_mode === 'manual' && a.formulas.length > 0 && (
              <p className="mb-3 rounded bg-amber-50 px-3 py-2 text-xs text-amber-800">算出方式が「直接入力」のため、計算式は金額の算出に使われません。</p>
            )}
            {a.formulas.length === 0 ? (
              <Empty>
                計算式はありません。
                {a.calc_mode === 'formula' ? '科目ごとに、ドライバーから金額を算出する式を登録します。' : '算出方式を「計算式」にすると、ドライバーから金額を算出できます。'}
              </Empty>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <th className="w-48">科目</th>
                    <th>計算式</th>
                    {canEdit && <th className="w-28" />}
                  </tr>
                </thead>
                <tbody>
                  {a.formulas.map((f) => {
                    const s = subjectById.get(f.subject_id)
                    return (
                      <tr key={f.id}>
                        <td>
                          {s?.name}
                          {s && <span className="ml-1 text-xs text-slate-400">{categoryLabels[s.category]}</span>}
                        </td>
                        <td className="font-mono text-xs">{f.expression}</td>
                        {canEdit && (
                          <td className="text-right">
                            <Button size="sm" variant="ghost" onClick={() => setFormula(f)}>
                              編集
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => deleteFormula(f)}>
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
        </div>
      </div>

      {editing && (
        <ActivityFormDialog
          initial={a}
          functions={editableFunctions}
          users={users.data.items}
          onClose={() => setEditing(false)}
          save={async (body) => {
            const ok = await withReason((reason) => api.put(base, { ...body, reason }).then(() => undefined), '施策の変更理由')
            if (ok) {
              setEditing(false)
              await reload()
            }
          }}
        />
      )}
      {milestone && (
        <MilestoneDialog
          initial={milestone === 'new' ? null : milestone}
          onClose={() => setMilestone(null)}
          save={async (body) => {
            const ok =
              milestone === 'new'
                ? (await api.post(`${base}/milestones`, body), true)
                : await withReason((reason) => api.put(`${base}/milestones/${milestone.id}`, { ...body, reason }).then(() => undefined), '期日の変更理由')
            if (ok) {
              setMilestone(null)
              await reload()
            }
          }}
        />
      )}
      {driver && (
        <DriverDialog
          initial={driver === 'new' ? null : driver}
          onClose={() => setDriver(null)}
          save={async (body) => {
            if (driver === 'new') await api.post(`${base}/drivers`, body)
            else await api.put(`${base}/drivers/${driver.id}`, body)
            setDriver(null)
            await reload()
          }}
        />
      )}
      {formula && (
        <FormulaDialog
          initial={formula === 'new' ? null : formula}
          subjects={subjects.data.items.filter((s) => formula !== 'new' || !a.formulas.some((f) => f.subject_id === s.id))}
          drivers={a.drivers}
          onClose={() => setFormula(null)}
          save={async (subjectId, body) => {
            await api.put(`${base}/formulas/${subjectId}`, body)
            setFormula(null)
            await reload()
          }}
        />
      )}
      <ConfirmDialog
        open={deletingDriver !== null}
        title="ドライバーの削除"
        message={<>「{deletingDriver?.name}」を削除します。計算式で使われているか、値が登録されている場合は削除できません。</>}
        reason="optional"
        onClose={() => setDeletingDriver(null)}
        onConfirm={async (reason) => {
          await api.del(`${base}/drivers/${deletingDriver!.id}`, { reason })
          await reload()
        }}
      />
      {reasonDialog}
    </>
  )
}

function Info({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-slate-500">{label}</dt>
      <dd className="text-slate-800">{children}</dd>
    </>
  )
}
