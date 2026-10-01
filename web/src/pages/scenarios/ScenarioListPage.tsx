import { planRoleLabels, type ActivityDetail, type List, type PlanRole, type Scenario } from '../../api/types'
import { Badge, Card, Empty, ErrorMessage, Loading, PageHeader, Table } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link, useLocation } from '../../lib/router'
import { actualThroughLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

const roleTone: Record<PlanRole, 'indigo' | 'green' | 'amber'> = { initial: 'indigo', revised: 'amber', latest: 'green' }

/** シナリオの状態（エイリアス・作成中・ロック・決算確定月） */
export function ScenarioBadges({ s }: { s: Scenario }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {s.plan_role && <Badge tone={roleTone[s.plan_role]}>{planRoleLabels[s.plan_role]}</Badge>}
      {s.is_active && <Badge tone="green">✎ 作成中</Badge>}
      {s.is_locked && <Badge tone="amber">🔒 ロック済み</Badge>}
      <Badge tone="slate">{actualThroughLabel(s.actual_through)}</Badge>
    </span>
  )
}

/**
 * シナリオ一覧。クエリ activity_id があるときは、その施策の数値を開くシナリオを選ぶ画面として使う。
 */
export function ScenarioListPage() {
  const me = useCurrentUser()
  const canWrite = me.role === 'fpa_admin'
  const { search } = useLocation()
  const activityId = search.get('activity_id')
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const activity = useApi<ActivityDetail>(activityId ? `/activities/${activityId}` : null)

  const byId = new Map((scenarios.data?.items ?? []).map((s) => [s.id, s]))
  const target = (s: Scenario) => (activityId ? `/scenarios/${s.id}/activities/${activityId}` : `/scenarios/${s.id}`)

  return (
    <>
      <PageHeader
        title="シナリオ"
        description="計画・見込の版をシナリオとして管理します。決算確定月以前の月は実績、それより後の月は計画値です。見込は既存のシナリオを複製して作ります。"
        actions={
          canWrite && (
            <Link
              to="/admin/scenarios"
              className="inline-flex items-center rounded-md border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              シナリオ管理（作成・実績取込）
            </Link>
          )
        }
      />
      {activityId && (
        <div className="mb-4 rounded-md border border-indigo-200 bg-indigo-50 px-4 py-3 text-sm text-indigo-800">
          施策「{activity.data?.name ?? '…'}」の数値を表示するシナリオを選んでください。
          <Link to={`/activities/${activityId}`} className="ml-2 underline">
            施策に戻る
          </Link>
        </div>
      )}
      <Card>
        {scenarios.error ? (
          <ErrorMessage error={scenarios.error} />
        ) : !scenarios.data ? (
          <Loading />
        ) : scenarios.data.items.length === 0 ? (
          <Empty>シナリオがありません{canWrite ? '。シナリオ管理から作成してください' : ''}</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>シナリオ名</th>
                <th>年度</th>
                <th>状態</th>
                <th>複製元</th>
              </tr>
            </thead>
            <tbody>
              {scenarios.data.items.map((s) => (
                <tr key={s.id} className="hover:bg-slate-50">
                  <td>
                    <Link to={target(s)} className="font-medium text-indigo-700 hover:underline">
                      {s.name}
                    </Link>
                  </td>
                  <td className="tabular-nums">{s.fiscal_year}年度</td>
                  <td>
                    <ScenarioBadges s={s} />
                  </td>
                  <td className="text-slate-500">{s.base_scenario_id ? byId.get(s.base_scenario_id)?.name : ''}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </>
  )
}
