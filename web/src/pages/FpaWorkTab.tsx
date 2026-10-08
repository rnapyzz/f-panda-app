import { useState } from 'react'
import { api, query } from '../api/client'
import { noteStatusLabels, type ActivityProgressReport, type List, type Scenario, type ScenarioDrift, type UnallocatedList, type Unit, type User } from '../api/types'
import { Badge, Button, Card, Empty, ErrorMessage, Loading, Table, cx } from '../components/ui'
import { buildChecklist, pendingOwners, progressByUnit, type StepKey, type StepState } from '../lib/fpaHome'
import { formatDateTime } from '../lib/format'
import { countByStatus } from '../lib/home'
import { Link } from '../lib/router'
import { currentFiscalYear, deadlineStatus, todayInTokyo } from '../lib/scenario'
import { useApi } from '../lib/useApi'
import { Help } from '../components/Help'

const stepLinks: Partial<Record<StepKey, { to: string; label: string }>> = {
  import: { to: '/admin/scenarios', label: '実績を取り込む' },
  allocate: { to: '/admin/actuals', label: '実績の割当を開く' },
  start: { to: '/admin/scenarios', label: '月次の見込を始める' },
  deadline: { to: '/admin/scenarios', label: '締切を入れる' },
}
const stateLabels: Record<StepState, string> = { done: '済み', current: '今ここ', todo: 'まだ' }
const stateTone = { done: 'green', current: 'indigo', todo: 'slate' } as const
const statusTone = { not_started: 'slate', in_progress: 'amber', completed: 'green' } as const

/**
 * FP&A のホームの「今月の作業」（docs/plan.md「2.20」）。
 * 月次のサイクルのチェックリスト（データから自動で判定）と、現場の更新の状況（ユニット別・担当者別、催促）。
 */
export function FpaWorkTab({ active }: { active: Scenario | null }) {
  const fy = active?.fiscal_year ?? currentFiscalYear()
  const months = useApi<{ months: string[] }>(`/actuals/months?fiscal_year=${fy}`)
  const unallocated = useApi<UnallocatedList>(`/actuals/unallocated?fiscal_year=${fy}`)
  const drift = useApi<List<ScenarioDrift>>('/scenarios/actual-drift')
  const report = useApi<ActivityProgressReport>(active ? `/scenarios/${active.id}/activity-status${query({ scope: 'all' })}` : null)
  const reminded = useApi<{ user_ids: number[] }>(active ? `/scenarios/${active.id}/reminders` : null)
  const units = useApi<List<Unit>>('/units')
  const users = useApi<List<User>>('/users')

  const error = months.error ?? unallocated.error ?? drift.error ?? report.error ?? units.error ?? users.error
  if (error) return <ErrorMessage error={error} />
  if (!months.data || !unallocated.data || !drift.data || !units.data || !users.data || (active && !report.data)) return <Loading />

  const items = report.data?.items ?? []
  const counts = countByStatus(items)
  const today = todayInTokyo()
  const { steps, next } = buildChecklist({
    lastMonth: months.data.months.at(-1) ?? null,
    unallocated: { count: unallocated.data.count, amount: BigInt(unallocated.data.revenue) + BigInt(unallocated.data.expense) },
    active,
    progress: { total: items.length, completed: counts.completed },
    today,
  })
  const unitById = new Map(units.data.items.map((u) => [u.id, u]))
  const userName = new Map(users.data.items.map((u) => [u.id, u.name]))
  const rate = items.length === 0 ? null : Math.round((counts.completed / items.length) * 100)
  const deadline = deadlineStatus(active?.update_deadline ?? null, today)

  return (
    <div className="space-y-4">
      {drift.data.items.length > 0 && (
        <p className="rounded-md border border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-900" role="status" aria-label="実績の修正">
          締めた後に実績が修正され、ロック済みのシナリオと食い違う月があります（{drift.data.items.map((d) => d.name).join('、')}）。
          <Link to="/admin/scenarios" className="ml-1 font-medium underline">
            シナリオ管理で確認する →
          </Link>
        </p>
      )}

      <Card
        title={
          <>
            今月の作業（{fy}年度）
            <Help manual="fpa#cycle">月次のサイクルの各ステップを、データから自動で「済み・今ここ・まだ」に判定します。「今ここ」のリンクから作業してください。</Help>
          </>
        }
      >
        <ol className="space-y-2" aria-label="サイクルのチェックリスト">
          {steps.map((s, i) => {
            const link = stepLinks[s.key]
            return (
              <li
                key={s.key}
                aria-current={s.state === 'current' ? 'step' : undefined}
                className={cx('flex flex-wrap items-center gap-3 rounded-md border px-3 py-2', s.state === 'current' ? 'border-indigo-300 bg-indigo-50/60' : 'border-slate-200')}
              >
                <span className={cx('flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold', s.state === 'done' ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600')}>
                  {s.state === 'done' ? '✓' : i + 1}
                </span>
                <span className={cx('min-w-48 font-medium', s.state === 'done' ? 'text-slate-500' : 'text-slate-800')}>{s.label}</span>
                <Badge tone={stateTone[s.state]}>{stateLabels[s.state]}</Badge>
                <span className="flex-1 text-sm text-slate-600">{s.detail}</span>
                {link && s.state !== 'done' && (
                  <Link to={link.to} className="text-sm font-medium text-indigo-700 hover:underline">
                    {link.label} →
                  </Link>
                )}
              </li>
            )
          })}
        </ol>
        {next && <p className="mt-3 text-sm text-emerald-800">✓ 今月の作業はすべて済みました。{next}</p>}
      </Card>

      <Card title="更新の状況">
        {!active ? (
          <Empty>今回の見込がまだありません。月次の見込を始めると、現場の更新の状況がここに表示されます。</Empty>
        ) : (
          <div className="space-y-5">
            <div className="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm" aria-label="全体の更新の状況">
              <span className="text-2xl font-semibold tabular-nums">{rate === null ? '—' : `${rate}%`}</span>
              <span className="text-slate-600">
                完了 {counts.completed} / {items.length} 件
              </span>
              {(['not_started', 'in_progress', 'completed'] as const).map((st) => (
                <span key={st}>
                  <Badge tone={statusTone[st]}>{noteStatusLabels[st]}</Badge> <span className="tabular-nums">{counts[st]}</span>
                </span>
              ))}
              {deadline && <span className={cx(deadline.tone === 'overdue' ? 'text-red-600' : deadline.tone === 'soon' ? 'text-amber-700' : 'text-slate-600')}>{deadline.text}</span>}
            </div>

            <section aria-label="ユニット別">
              <h3 className="mb-2 text-xs font-semibold text-slate-500">ユニット別</h3>
              <Table>
                <thead>
                  <tr>
                    <th>ユニット</th>
                    <th>マネージャー</th>
                    <th className="text-right">施策</th>
                    <th className="text-right">未着手</th>
                    <th className="text-right">入力中</th>
                    <th className="text-right">完了</th>
                    <th className="w-48">完了率</th>
                  </tr>
                </thead>
                <tbody>
                  {progressByUnit(items).map((u) => {
                    const unit = unitById.get(u.unitId)
                    return (
                      <tr key={u.unitId}>
                        <td className="font-medium">{unit?.name}</td>
                        <td className="text-slate-600">{unit?.owner_user_id ? userName.get(unit.owner_user_id) : '—'}</td>
                        <td className="text-right tabular-nums">{u.total}</td>
                        <td className="text-right tabular-nums">{u.counts.not_started}</td>
                        <td className="text-right tabular-nums">{u.counts.in_progress}</td>
                        <td className="text-right tabular-nums">{u.counts.completed}</td>
                        <td>
                          <div className="flex items-center gap-2">
                            <div className="h-2 flex-1 overflow-hidden rounded-full bg-slate-100" aria-hidden="true">
                              <div className="h-full rounded-full bg-emerald-500" style={{ width: `${Math.round(u.rate * 100)}%` }} />
                            </div>
                            <span className="w-10 text-right text-xs tabular-nums">{Math.round(u.rate * 100)}%</span>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </Table>
            </section>

            <PendingOwners scenarioId={active.id} owners={pendingOwners(items)} userName={userName} reminded={new Set(reminded.data?.user_ids ?? [])} onSent={reminded.reload} />
          </div>
        )}
      </Card>
    </div>
  )
}

function PendingOwners({
  scenarioId,
  owners,
  userName,
  reminded,
  onSent,
}: {
  scenarioId: number
  owners: ReturnType<typeof pendingOwners>
  userName: Map<number, string>
  reminded: Set<number>
  onSent: () => Promise<void>
}) {
  const [sending, setSending] = useState<number | null>(null)
  const [error, setError] = useState<unknown>(null)
  const remind = async (userId: number) => {
    setSending(userId)
    setError(null)
    try {
      await api.post(`/scenarios/${scenarioId}/reminders`, { user_id: userId })
      await onSent()
    } catch (err) {
      setError(err)
    } finally {
      setSending(null)
    }
  }
  return (
    <section aria-label="未完了の担当者">
      <h3 className="mb-2 text-xs font-semibold text-slate-500">未完了の担当者</h3>
      {error ? (
        <div className="mb-2">
          <ErrorMessage error={error} />
        </div>
      ) : null}
      {owners.length === 0 ? (
        <p className="text-sm text-emerald-700">未完了の施策はありません。</p>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>担当者</th>
              <th className="text-right">未着手</th>
              <th className="text-right">入力中</th>
              <th>最後に更新</th>
              <th className="w-36" />
            </tr>
          </thead>
          <tbody>
            {owners.map((o) => (
              <tr key={o.userId ?? 'none'}>
                <td className="font-medium">{o.userId === null ? <span className="text-amber-700">担当者なし</span> : userName.get(o.userId)}</td>
                <td className="text-right tabular-nums">{o.notStarted}</td>
                <td className="text-right tabular-nums">{o.inProgress}</td>
                <td className="text-xs text-slate-500">{o.lastEditedAt ? formatDateTime(o.lastEditedAt) : '—'}</td>
                <td className="text-right">
                  {o.userId === null ? (
                    <Link to="/activities" className="text-sm text-indigo-700 hover:underline">
                      施策の一覧で担当を決める →
                    </Link>
                  ) : reminded.has(o.userId) ? (
                    <span className="text-xs text-slate-500">本日送信済み</span>
                  ) : (
                    <Button size="sm" disabled={sending !== null} onClick={() => remind(o.userId!)} aria-label={`${userName.get(o.userId)}に催促する`}>
                      {sending === o.userId ? '送信中…' : '催促する'}
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </section>
  )
}
