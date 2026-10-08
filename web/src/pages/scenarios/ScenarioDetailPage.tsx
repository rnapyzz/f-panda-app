import { useMemo, useState } from 'react'
import { activityTypeLabels, type Activity, type List, type Scenario, type Unit } from '../../api/types'
import { Card, Empty, ErrorMessage, Input, Loading, PageHeader, Table } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { PlanValuesCsvCard } from './PlanValuesCsv'
import { ScenarioBadges } from './ScenarioListPage'

export function ScenarioDetailPage({ id }: { id: string }) {
  const me = useCurrentUser()
  const isAdmin = me.role === 'fpa_admin'
  const scenario = useApi<Scenario>(`/scenarios/${id}`)
  const activities = useApi<List<Activity>>('/activities')
  const units = useApi<List<Unit>>('/units')
  const unitName = useMemo(() => new Map((units.data?.items ?? []).map((f) => [f.id, f.name])), [units.data])
  const [q, setQ] = useState('')

  const error = scenario.error ?? activities.error ?? units.error
  if (error) return <ErrorMessage error={error} />
  if (!scenario.data || !activities.data || !units.data) return <Loading />
  const s = scenario.data

  const keyword = q.trim().toLowerCase()
  const shown = activities.data.items.filter((a) => !keyword || a.code.toLowerCase().includes(keyword) || a.name.toLowerCase().includes(keyword))

  return (
    <>
      <div className="mb-2 text-sm">
        <Link to="/scenarios" className="text-slate-500 hover:text-slate-700">
          ← シナリオ一覧
        </Link>
      </div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-2">
            {s.name}
            <ScenarioBadges s={s} />
          </span>
        }
        description={`${s.fiscal_year}年度（${s.fiscal_year}年4月〜${s.fiscal_year + 1}年3月）`}
        actions={
          <>
            <Link
              to={`/history?scenario_id=${s.id}`}
              className="inline-flex items-center rounded-md border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
            >
              変更履歴
            </Link>
            {isAdmin && (
              <Link
                to="/admin/scenarios"
                className="inline-flex items-center rounded-md border border-slate-300 bg-white px-3.5 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
              >
                シナリオ管理で変更
              </Link>
            )}
          </>
        }
      />
      {s.is_locked && <p className="mb-4 rounded-md bg-amber-50 px-4 py-2 text-sm text-amber-800">このシナリオはロックされています。数値は変更できません。</p>}
      {!s.is_locked && !s.is_active && (
        <p className="mb-4 rounded-md bg-slate-100 px-4 py-2 text-sm text-slate-700">このシナリオは今回の見込ではないため、数値を入力できるのは FP&A のみです。</p>
      )}

      <PlanValuesCsvCard scenario={s} units={units.data.items} canImport={!s.is_locked && (s.is_active ? me.role !== 'viewer' : isAdmin)} />

      <Card
        title="施策"
        actions={<Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="コード・名称で絞り込み" className="w-56 py-1" aria-label="施策の絞り込み" />}
      >
        {shown.length === 0 ? (
          <Empty>施策がありません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-32">コード</th>
                <th>施策名</th>
                <th>タイプ</th>
                <th>ユニット</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {shown.map((a) => (
                <tr key={a.id} className="hover:bg-slate-50">
                  <td className="font-mono text-xs">{a.code}</td>
                  <td className="font-medium">{a.name}</td>
                  <td className="text-slate-600">{activityTypeLabels[a.activity_type]}</td>
                  <td className="text-slate-600">{unitName.get(a.unit_id)}</td>
                  <td className="text-right">
                    <Link to={`/activities/${a.id}?tab=update&scenario=${s.id}`} className="text-sm font-medium text-indigo-700 hover:underline">
                      {a.can_edit && !s.is_locked && (s.is_active || isAdmin) ? '数値を入力' : '数値を見る'} →
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

    </>
  )
}
