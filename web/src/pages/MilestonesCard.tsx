import { milestoneStatusLabels, type ActivityProgress, type MilestoneStatus } from '../api/types'
import { Badge, Card, Empty, ErrorMessage, Loading, Table } from '../components/ui'
import { Link } from '../lib/router'
import { useApi } from '../lib/useApi'

type HomeMilestone = {
  id: number
  activity_id: number
  name: string
  due_date: string
  status: MilestoneStatus
  overdue: boolean
  delayed: boolean
  upcoming: boolean
  postponed: { count: number; days: number }
}

/** 対象になった理由 */
export type MilestoneTarget = { item: ActivityProgress; reasons: ('変動上位' | '重点' | 'ウォッチ')[] }

/**
 * マイルストーン（docs/plan.md「2.11」の ③）。変動上位・重点施策・ウォッチの施策の、完了していないマイルストーンを期日の順に並べる。
 */
export function MilestonesCard({ scenarioId, targets }: { scenarioId: number; targets: MilestoneTarget[] }) {
  const ids = targets.map((t) => t.item.activity_id).sort((a, b) => a - b)
  const data = useApi<{ today: string; items: HomeMilestone[] }>(ids.length > 0 ? `/scenarios/${scenarioId}/milestones?activity_ids=${ids.join(',')}` : null)
  const byId = new Map(targets.map((t) => [t.item.activity_id, t]))

  return (
    <Card title="マイルストーン（変動上位・重点施策・ウォッチ）" className="mb-4">
      {ids.length === 0 ? (
        <Empty>対象の施策がありません。施策を重点施策にするか、☆でウォッチすると表示されます。</Empty>
      ) : data.error ? (
        <ErrorMessage error={data.error} />
      ) : !data.data ? (
        <Loading />
      ) : data.data.items.length === 0 ? (
        <Empty>完了していないマイルストーンはありません。</Empty>
      ) : (
        <Table>
          <thead>
            <tr>
              <th className="w-28">期日</th>
              <th>マイルストーン</th>
              <th>施策</th>
              <th className="w-20">状態</th>
              <th>注意</th>
            </tr>
          </thead>
          <tbody>
            {data.data.items.map((m) => {
              const t = byId.get(m.activity_id)
              return (
                <tr key={m.id}>
                  <td className="tabular-nums">{m.due_date}</td>
                  <td className="font-medium">{m.name}</td>
                  <td>
                    <Link to={`/activities/${m.activity_id}`} className="text-indigo-700 hover:underline">
                      {t?.item.name}
                    </Link>
                    <span className="ml-1.5 inline-flex gap-1">
                      {t?.reasons.map((r) => (
                        <span key={r} className="rounded bg-slate-100 px-1 text-[11px] text-slate-500">
                          {r}
                        </span>
                      ))}
                    </span>
                  </td>
                  <td className="whitespace-nowrap text-slate-600">{milestoneStatusLabels[m.status]}</td>
                  <td>
                    <span className="flex flex-wrap gap-1">
                      {m.overdue && <Badge tone="red">期日超過</Badge>}
                      {m.delayed && <Badge tone="red">遅延</Badge>}
                      {m.upcoming && <Badge tone="amber">30日以内</Badge>}
                      {m.postponed.count > 0 && (
                        <Badge tone="amber">
                          後ろ倒し {m.postponed.count}回・+{m.postponed.days}日
                        </Badge>
                      )}
                    </span>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </Table>
      )}
    </Card>
  )
}
