import { CsvActions } from '../../components/CsvTransfer'
import { useMemo, useState, type ReactNode } from 'react'
import { query } from '../../api/client'
import { activityStatusLabels, activityTypeLabels, type Activity, type ActivityStatus, type ConfidenceLevel, type Unit, type List, type User } from '../../api/types'
import { Badge, Button, Card, Empty, ErrorMessage, Input, Loading, PageHeader, Select, Table } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link, navigate, useLocation } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { confidenceLabel } from '../../lib/confidence'
import { ActivityFormDialog, createActivity } from './ActivityFormDialog'

export const statusTone: Record<ActivityStatus, 'slate' | 'indigo' | 'green' | 'amber' | 'red'> = {
  planned: 'slate',
  in_progress: 'indigo',
  completed: 'green',
  on_hold: 'amber',
  cancelled: 'red',
}

/** 施策を作成できるユニット（FP&A は全ユニット、マネージャーは担当のユニット） */
export function creatableUnits(user: { id: number; role: string }, units: Unit[]): Unit[] {
  if (user.role === 'fpa_admin') return units
  if (user.role === 'manager') return units.filter((f) => f.owner_user_id === user.id)
  return []
}

export function ActivityListPage() {
  const me = useCurrentUser()
  const { search } = useLocation()
  const filters = {
    unit_id: search.get('unit_id') ?? '',
    activity_type: search.get('activity_type') ?? '',
    status: search.get('status') ?? '',
    owner_user_id: search.get('owner_user_id') ?? '',
    q: search.get('q') ?? '',
  }
  const [q, setQ] = useState(filters.q)

  const activities = useApi<List<Activity>>(`/activities${query(filters)}`)
  const levels = useApi<List<ConfidenceLevel>>('/confidence-levels')
  const units = useApi<List<Unit>>('/units')
  const users = useApi<List<User>>('/users')
  const unitName = useMemo(() => new Map((units.data?.items ?? []).map((f) => [f.id, f.name])), [units.data])
  const userName = useMemo(() => new Map((users.data?.items ?? []).map((u) => [u.id, u.name])), [users.data])
  const creatable = creatableUnits(me, units.data?.items ?? [])
  const [creating, setCreating] = useState(false)

  // 絞り込み条件は URL に持たせる（戻る・共有で同じ一覧を表示できるように）
  const setFilter = (key: string, value: string) => {
    const next = { ...filters, [key]: value }
    navigate(`/activities${query(next)}`, { replace: true })
  }

  const error = activities.error ?? units.error ?? users.error
  return (
    <>
      <PageHeader
        title="施策"
        description="予実管理の単位となる施策（案件・運用・コストプール）の一覧です。"
        actions={
          <>
            <CsvActions
              resource="activities"
              label="施策"
              canImport={me.role === 'fpa_admin'}
              columns="code,name,unit_code,activity_type,status,start_date,end_date,owner_email,confidence_level,assumptions,external_codes"
              notes={
                <>
                  <p>code が空の行は新しい施策として追加し、施策コードを自動で採番します。</p>
                  <p>confidence_level は確度の段階のコード（例: C、空なら施策タイプの既定）、日付は YYYY-MM-DD。external_codes は外部コードを空白区切りで書くと施策に追加します（書いていない外部コードは外しません）。</p>
                </>
              }
              onImported={() => activities.reload()}
            />
            {creatable.length > 0 && (
              <Button variant="primary" onClick={() => setCreating(true)}>
                ＋ 施策を追加
              </Button>
            )}
          </>
        }
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
              キーワード（コード・名称・外部コード）
            </label>
            <Input id="activity-q" value={q} onChange={(e) => setQ(e.target.value)} onBlur={() => q !== filters.q && setFilter('q', q)} placeholder="Enter で検索" />
          </form>
          <FilterSelect label="ユニット" value={filters.unit_id} onChange={(v) => setFilter('unit_id', v)}>
            {(units.data?.items ?? []).map((f) => (
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
                <th>ユニット</th>
                <th>担当者</th>
                <th className="text-right">確度</th>
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
                  <td className="text-slate-600">{unitName.get(a.unit_id)}</td>
                  <td className="text-slate-600">{a.owner_user_id ? userName.get(a.owner_user_id) : ''}</td>
                  <td className="text-right whitespace-nowrap tabular-nums">{confidenceLabel(a.confidence_level, levels.data?.items)}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {creating && units.data && users.data && (
        <ActivityFormDialog
          initial={null}
          units={creatable}
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
