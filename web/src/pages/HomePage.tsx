import { useState } from 'react'
import { query } from '../api/client'
import { noteStatusLabels, type ActivityProgress, type ActivityProgressReport, type List, type Unit, type User } from '../api/types'
import { Badge, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../components/ui'
import { PriorityBadge, WatchButton } from '../components/PriorityWatch'
import { useActiveScenario } from '../lib/activeScenario'
import { useCurrentUser } from '../lib/auth'
import { formatDateTime, formatYen, monthLabel } from '../lib/format'
import { countByStatus, profitDiff, profitOf, sortStatuses, type SortMode } from '../lib/home'
import { Link } from '../lib/router'
import { actualThroughLabel, scenarioLabel } from '../lib/scenario'
import { useApi } from '../lib/useApi'
import { ServiceStatusCard } from './ServiceStatusCard'

type Scope = ActivityProgressReport['scope']

const scopeLabels: Record<Scope, string> = { mine: '自分の担当', units: '所管ユニット', all: 'すべて' }
const statusTone = { not_started: 'slate', in_progress: 'amber', completed: 'green' } as const

/**
 * ホーム（docs/plan.md「2.10 現場担当の動線」）。作成中のシナリオについて、施策ごとの状態と、基準・前回見込との差を一覧にする。
 * 実績が新しく反映されたときは、前回見込との差が大きい施策を知らせる。
 */
export function HomePage() {
  const { active } = useActiveScenario()
  if (active === undefined) return <Loading />
  if (active === null) {
    return (
      <>
        <PageHeader title="ホーム" />
        <Card>
          <Empty>作成中のシナリオがありません。FP&A がシナリオ管理で作成中のシナリオを指定すると、更新する施策がここに表示されます。</Empty>
        </Card>
      </>
    )
  }
  return <HomeView key={active.id} scenarioId={active.id} />
}

function HomeView({ scenarioId }: { scenarioId: number }) {
  const me = useCurrentUser()
  const scopes: Scope[] = me.role === 'member' ? ['mine', 'all'] : me.role === 'manager' ? ['units', 'mine', 'all'] : ['all', 'mine']
  const [scope, setScope] = useState<Scope>(scopes[0])
  const [unitId, setUnitId] = useState('')
  const [sort, setSort] = useState<SortMode>('status')
  const report = useApi<ActivityProgressReport>(`/scenarios/${scenarioId}/activity-status${query({ scope })}`)
  const units = useApi<List<Unit>>('/units')
  const users = useApi<List<User>>('/users')

  const error = report.error ?? units.error ?? users.error
  if (error) return <ErrorMessage error={error} />
  const r = report.data
  if (!r || !units.data || !users.data) return <Loading />

  const unitName = new Map(units.data.items.map((u) => [u.id, u.name]))
  const userName = new Map(users.data.items.map((u) => [u.id, u.name]))
  const filtered = r.items.filter((it) => !unitId || String(it.unit_id) === unitId)
  const items = sortStatuses(filtered, sort)
  const counts = countByStatus(filtered)
  const shownUnits = [...new Set(r.items.map((it) => it.unit_id))]
  const largeMisses = r.new_actual_months.length > 0 ? sortStatuses(filtered.filter((it) => it.accuracy?.large), 'previous') : []

  return (
    <>
      <PageHeader
        title="ホーム"
        description={
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span>
              作成中: <Link to={`/scenarios/${r.scenario.id}`} className="font-medium text-indigo-700 hover:underline">{scenarioLabel(r.scenario)}</Link>（{actualThroughLabel(r.scenario.actual_through)}）
            </span>
            <span>基準: {r.base ? scenarioLabel(r.base) : '未設定'}</span>
            <span>前回見込: {r.previous ? scenarioLabel(r.previous) : '未設定'}</span>
          </span>
        }
      />

      {r.new_actual_months.length > 0 && (
        <Card className="mb-4">
          <div role="status" aria-label="実績のお知らせ">
            <p className="text-sm font-medium text-slate-800">
              📥 {r.new_actual_months.map(monthLabel).join('・')}の実績が反映されました。前回見込との差を確認して、見込を更新してください。
            </p>
            {largeMisses.length > 0 ? (
              <ul className="mt-2 space-y-1 text-sm">
                <li className="text-xs text-slate-500">前回見込との差が大きい施策（差が前回見込の 20% 以上）</li>
                {largeMisses.map((it) => (
                  <li key={it.activity_id} className="flex flex-wrap items-baseline gap-x-2">
                    <Link to={`/scenarios/${r.scenario.id}/activities/${it.activity_id}`} className="font-medium text-indigo-700 hover:underline">
                      {it.name}
                    </Link>
                    <span className="text-xs text-slate-500 tabular-nums">
                      前回見込 {formatYen(String(profitOf(it.accuracy!.plan)))} → 実績 {formatYen(String(profitOf(it.accuracy!.actual)))}（利益）
                      {it.accuracy!.rate !== null && `・差 ${it.accuracy!.rate}%`}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-1 text-xs text-slate-500">前回見込との差が大きい施策はありません。</p>
            )}
          </div>
        </Card>
      )}

      {scope !== 'mine' && <ServiceStatusCard report={r} items={r.items} units={units.data.items} selectedUnit={unitId} onSelectUnit={setUnitId} />}

      <Card
        title={
          <span className="flex flex-wrap items-center gap-3">
            更新する施策
            <span className="flex gap-2 text-xs font-normal text-slate-500" aria-label="状態ごとの件数">
              {(['not_started', 'in_progress', 'completed'] as const).map((s) => (
                <span key={s}>
                  <Badge tone={statusTone[s]}>{noteStatusLabels[s]}</Badge> {counts[s]}
                </span>
              ))}
            </span>
          </span>
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div role="group" aria-label="範囲" className="inline-flex rounded-md border border-slate-300 bg-white p-0.5">
              {scopes.map((s) => (
                <button
                  key={s}
                  type="button"
                  aria-pressed={scope === s}
                  onClick={() => {
                    setScope(s)
                    setUnitId('')
                  }}
                  className={cx('rounded px-2.5 py-1 text-xs font-medium', scope === s ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
                >
                  {scopeLabels[s]}
                </button>
              ))}
            </div>
            {shownUnits.length > 1 && (
              <div className="w-40">
              <Select aria-label="ユニット" value={unitId} onChange={(e) => setUnitId(e.target.value)} className="py-1 text-xs">
                <option value="">すべてのユニット</option>
                {shownUnits.map((id) => (
                  <option key={id} value={id}>
                    {unitName.get(id)}
                  </option>
                ))}
              </Select>
              </div>
            )}
            <div className="w-48">
            <Select aria-label="並べ替え" value={sort} onChange={(e) => setSort(e.target.value as SortMode)} className="py-1 text-xs">
              <option value="status">未完了を先に</option>
              <option value="base">基準との差の大きい順</option>
              <option value="previous">前回見込との差の大きい順</option>
            </Select>
            </div>
          </div>
        }
      >
        {items.length === 0 ? (
          <Empty>{scope === 'mine' ? '担当している施策はありません。' : '施策はありません。'}</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>施策</th>
                <th>ユニット</th>
                <th>担当者</th>
                <th>状態</th>
                <th className="text-right">今回の利益（年間）</th>
                <th className="text-right">基準との差</th>
                <th className="text-right">前回見込との差</th>
                <th>最終更新</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <Row key={it.activity_id} it={it} scenarioId={r.scenario.id} unit={unitName.get(it.unit_id)} owner={it.owner_user_id ? userName.get(it.owner_user_id) : undefined} />
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </>
  )
}

function Row({ it, scenarioId, unit, owner }: { it: ActivityProgress; scenarioId: number; unit?: string; owner?: string }) {
  const diff = (compare: typeof it.base) => {
    const d = profitDiff(it.current, compare)
    if (d === null) return <span className="text-slate-300">—</span>
    return <span className={cx(d > 0n && 'text-emerald-700', d < 0n && 'text-red-600', d === 0n && 'text-slate-400')}>{`${d > 0n ? '+' : ''}${formatYen(String(d))}`}</span>
  }
  return (
    <tr>
      <td>
        <span className="inline-flex items-center gap-1.5">
          <WatchButton activityId={it.activity_id} name={it.name} watched={it.is_watched} />
          <Link to={`/scenarios/${scenarioId}/activities/${it.activity_id}`} className="font-medium text-indigo-700 hover:underline">
            {it.name}
          </Link>
          {it.is_priority && <PriorityBadge />}
        </span>
        <div className="pl-6 font-mono text-xs text-slate-400">{it.code}</div>
      </td>
      <td className="text-slate-600">{unit}</td>
      <td className="text-slate-600">{owner ?? '未設定'}</td>
      <td className="whitespace-nowrap">
        <Badge tone={statusTone[it.status]}>{noteStatusLabels[it.status]}</Badge>
        {it.has_explanation && (
          <span className="ml-1 text-xs text-slate-400" title="差異の説明あり">
            📝
          </span>
        )}
      </td>
      <td className="text-right tabular-nums">{formatYen(String(profitOf(it.current)))}</td>
      <td className="text-right tabular-nums">{diff(it.base)}</td>
      <td className="text-right tabular-nums">{diff(it.previous)}</td>
      <td className="text-xs whitespace-nowrap text-slate-500">{it.completed_at ? formatDateTime(it.completed_at) : it.last_edited_at ? formatDateTime(it.last_edited_at) : '—'}</td>
    </tr>
  )
}
