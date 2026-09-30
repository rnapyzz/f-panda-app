import { useMemo, useState, type ReactNode } from 'react'
import { query } from '../../api/client'
import { activityStatusLabels, activityTypeLabels, calcModeLabels, type Activity, type ActivityStatus, type FunctionItem, type List, type User } from '../../api/types'
import { Badge, Button, Card, Empty, ErrorMessage, Input, Loading, PageHeader, Select, Table } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatPercent } from '../../lib/format'
import { Link, navigate, useLocation } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { ActivityFormDialog, createActivity } from './ActivityFormDialog'

export const statusTone: Record<ActivityStatus, 'slate' | 'indigo' | 'green' | 'amber' | 'red'> = {
  planned: 'slate',
  in_progress: 'indigo',
  completed: 'green',
  on_hold: 'amber',
  cancelled: 'red',
}

/** 施策を作成できる機能（FP&A は全機能、マネージャーは担当の機能） */
export function creatableFunctions(user: { id: number; role: string }, functions: FunctionItem[]): FunctionItem[] {
  if (user.role === 'fpa_admin') return functions
  if (user.role === 'manager') return functions.filter((f) => f.owner_user_id === user.id)
  return []
}

export function ActivityListPage() {
  const me = useCurrentUser()
  const { search } = useLocation()
  const filters = {
    function_id: search.get('function_id') ?? '',
    activity_type: search.get('activity_type') ?? '',
    status: search.get('status') ?? '',
    owner_user_id: search.get('owner_user_id') ?? '',
    q: search.get('q') ?? '',
  }
  const [q, setQ] = useState(filters.q)

  const activities = useApi<List<Activity>>(`/activities${query(filters)}`)
  const functions = useApi<List<FunctionItem>>('/functions')
  const users = useApi<List<User>>('/users')
  const functionName = useMemo(() => new Map((functions.data?.items ?? []).map((f) => [f.id, f.name])), [functions.data])
  const userName = useMemo(() => new Map((users.data?.items ?? []).map((u) => [u.id, u.name])), [users.data])
  const creatable = creatableFunctions(me, functions.data?.items ?? [])
  const [creating, setCreating] = useState(false)

  // 絞り込み条件は URL に持たせる（戻る・共有で同じ一覧を表示できるように）
  const setFilter = (key: string, value: string) => {
    const next = { ...filters, [key]: value }
    navigate(`/activities${query(next)}`, { replace: true })
  }

  const error = activities.error ?? functions.error ?? users.error
  return (
    <>
      <PageHeader
        title="施策"
        description="予実管理の単位となる施策（案件・運用・コストプール）の一覧です。"
        actions={creatable.length > 0 && <Button variant="primary" onClick={() => setCreating(true)}>＋ 施策を追加</Button>}
      />

      <Card className="mb-4">
        <div className="flex flex-wrap items-end gap-3">
          <form
            className="min-w-60 flex-1"
            onSubmit={(e) => {
              e.preventDefault()
              setFilter('q', q)
            }}
          >
            <label className="mb-1 block text-xs font-medium text-slate-500" htmlFor="activity-q">
              キーワード（コード・名称）
            </label>
            <Input id="activity-q" value={q} onChange={(e) => setQ(e.target.value)} onBlur={() => q !== filters.q && setFilter('q', q)} placeholder="Enter で検索" />
          </form>
          <FilterSelect label="機能" value={filters.function_id} onChange={(v) => setFilter('function_id', v)}>
            {(functions.data?.items ?? []).map((f) => (
              <option key={f.id} value={f.id}>
                {f.name}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect label="タイプ" value={filters.activity_type} onChange={(v) => setFilter('activity_type', v)}>
            {Object.entries(activityTypeLabels).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </FilterSelect>
          <FilterSelect label="ステータス" value={filters.status} onChange={(v) => setFilter('status', v)}>
            {Object.entries(activityStatusLabels).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </FilterSelect>
          <label className="flex items-center gap-2 pb-2 text-sm text-slate-700">
            <input
              type="checkbox"
              className="size-4 rounded border-slate-300"
              checked={filters.owner_user_id === String(me.id)}
              onChange={(e) => setFilter('owner_user_id', e.target.checked ? String(me.id) : '')}
            />
            自分の担当のみ
          </label>
        </div>
      </Card>

      <Card>
        {error ? (
          <ErrorMessage error={error} />
        ) : !activities.data ? (
          <Loading />
        ) : activities.data.items.length === 0 ? (
          <Empty>条件に合う施策がありません</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th className="w-32">コード</th>
                <th>施策名</th>
                <th>タイプ</th>
                <th>ステータス</th>
                <th>機能</th>
                <th>担当者</th>
                <th className="text-right">確度</th>
                <th>算出方式</th>
              </tr>
            </thead>
            <tbody>
              {activities.data.items.map((a) => (
                <tr key={a.id} className="hover:bg-slate-50">
                  <td className="font-mono text-xs">{a.code}</td>
                  <td>
                    <Link to={`/activities/${a.id}`} className="font-medium text-indigo-700 hover:underline">
                      {a.name}
                    </Link>
                  </td>
                  <td className="text-slate-600">{activityTypeLabels[a.activity_type]}</td>
                  <td>
                    <Badge tone={statusTone[a.status]}>{activityStatusLabels[a.status]}</Badge>
                  </td>
                  <td className="text-slate-600">{functionName.get(a.function_id)}</td>
                  <td className="text-slate-600">{a.owner_user_id ? userName.get(a.owner_user_id) : ''}</td>
                  <td className="text-right tabular-nums">{formatPercent(a.probability)}</td>
                  <td className="text-slate-600">{calcModeLabels[a.calc_mode]}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {creating && functions.data && users.data && (
        <ActivityFormDialog
          initial={null}
          functions={creatable}
          users={users.data.items}
          onClose={() => setCreating(false)}
          save={async (body) => {
            const created = await createActivity(body)
            navigate(`/activities/${created.id}`)
          }}
        />
      )}
    </>
  )
}

function FilterSelect({ label, value, onChange, children }: { label: string; value: string; onChange: (v: string) => void; children: ReactNode }) {
  return (
    <div className="w-44">
      <label className="mb-1 block text-xs font-medium text-slate-500">
        {label}
        <Select value={value} onChange={(e) => onChange(e.target.value)} className="mt-1">
          <option value="">すべて</option>
          {children}
        </Select>
      </label>
    </div>
  )
}
