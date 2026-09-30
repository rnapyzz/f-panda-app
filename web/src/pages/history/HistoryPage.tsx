import { useEffect, useState, type ReactNode } from 'react'
import { api, query } from '../../api/client'
import {
  activityStatusLabels,
  activityTypeLabels,
  calcModeLabels,
  categoryLabels,
  driverKindLabels,
  milestoneStatusLabels,
  roleLabels,
  scenarioKindLabels,
  tableLabels,
  type Activity,
  type ChangeLog,
  type ChangeSet,
  type List,
  type Scenario,
  type User,
} from '../../api/types'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Input, Loading, PageHeader, Select, Table } from '../../components/ui'
import { formatDateTime, formatNumber, formatPercent, formatYen } from '../../lib/format'
import { Link, navigate, useLocation } from '../../lib/router'
import { useApi } from '../../lib/useApi'

type ListResponse = { items: ChangeSet[]; has_more: boolean }

const filterKeys = ['scenario_id', 'activity_id', 'user_id', 'reason', 'from', 'to'] as const

/** 変更履歴の一覧。誰が・いつ・なぜ・何を変えたかを確認する */
export function HistoryPage() {
  const { search } = useLocation()
  const filters = Object.fromEntries(filterKeys.map((k) => [k, search.get(k) ?? ''])) as Record<(typeof filterKeys)[number], string>
  const filterQuery = query(filters)

  const scenarios = useApi<List<Scenario>>('/scenarios')
  const activities = useApi<List<Activity>>('/activities')
  const users = useApi<List<User>>('/users')

  const [items, setItems] = useState<ChangeSet[] | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [selected, setSelected] = useState<ChangeSet | null>(null)

  useEffect(() => {
    let cancelled = false
    api
      .get<ListResponse>(`/change-sets${filterQuery}`)
      .then((res) => {
        if (cancelled) return
        setItems(res.items)
        setHasMore(res.has_more)
        setError(null)
      })
      .catch((err) => !cancelled && setError(err))
    return () => {
      cancelled = true
    }
  }, [filterQuery])

  const loadMore = async () => {
    if (!items?.length) return
    setLoadingMore(true)
    try {
      const sep = filterQuery ? '&' : '?'
      const res = await api.get<ListResponse>(`/change-sets${filterQuery}${sep}before_id=${items[items.length - 1].id}`)
      setItems([...items, ...res.items])
      setHasMore(res.has_more)
    } catch (err) {
      setError(err)
    } finally {
      setLoadingMore(false)
    }
  }

  const setFilter = (key: string, value: string) => {
    navigate(`/history${query({ ...filters, [key]: value })}`, { replace: true })
  }

  return (
    <>
      <PageHeader title="変更履歴" description="数値やマスタの変更を、変更した人・日時・理由とともに確認できます。行を選ぶと、何がどう変わったかを表示します。" />

      <Card className="mb-4">
        <div className="grid gap-3 md:grid-cols-6">
          <Filter label="シナリオ" className="md:col-span-2">
            <Select value={filters.scenario_id} onChange={(e) => setFilter('scenario_id', e.target.value)}>
              <option value="">すべて</option>
              {(scenarios.data?.items ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}（{scenarioKindLabels[s.scenario_kind]}）
                </option>
              ))}
            </Select>
          </Filter>
          <Filter label="施策" className="md:col-span-2">
            <Select value={filters.activity_id} onChange={(e) => setFilter('activity_id', e.target.value)}>
              <option value="">すべて</option>
              {(activities.data?.items ?? []).map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </Select>
          </Filter>
          <Filter label="変更した人" className="md:col-span-2">
            <Select value={filters.user_id} onChange={(e) => setFilter('user_id', e.target.value)}>
              <option value="">すべて</option>
              {(users.data?.items ?? []).map((u) => (
                <option key={u.id} value={u.id}>
                  {u.name}
                </option>
              ))}
            </Select>
          </Filter>
          <Filter label="変更理由" className="md:col-span-2">
            <Select value={filters.reason} onChange={(e) => setFilter('reason', e.target.value)}>
              <option value="">すべて</option>
              <option value="with">理由あり</option>
              <option value="without">理由なし</option>
            </Select>
          </Filter>
          <Filter label="期間（から）" className="md:col-span-2">
            <Input type="date" value={filters.from} onChange={(e) => setFilter('from', e.target.value)} />
          </Filter>
          <Filter label="期間（まで）" className="md:col-span-2">
            <Input type="date" value={filters.to} onChange={(e) => setFilter('to', e.target.value)} />
          </Filter>
        </div>
      </Card>

      <Card>
        {error ? (
          <ErrorMessage error={error} />
        ) : !items ? (
          <Loading />
        ) : items.length === 0 ? (
          <Empty>条件に合う変更履歴はありません</Empty>
        ) : (
          <>
            <Table>
              <thead>
                <tr>
                  <th className="w-36">日時</th>
                  <th className="w-32">変更した人</th>
                  <th>変更理由</th>
                  <th>対象</th>
                  <th className="w-40">内容</th>
                </tr>
              </thead>
              <tbody>
                {items.map((cs) => (
                  <tr key={cs.id} onClick={() => setSelected(cs)} className="cursor-pointer align-top hover:bg-indigo-50/40">
                    <td className="text-xs whitespace-nowrap text-slate-600 tabular-nums">{formatDateTime(cs.created_at)}</td>
                    <td className="whitespace-nowrap">{cs.user.name}</td>
                    <td>
                      {cs.reason ? <span className="whitespace-pre-wrap text-slate-800">{cs.reason}</span> : <Badge tone="amber">理由なし</Badge>}
                      {cs.scenario && <div className="mt-0.5 text-xs text-slate-500">シナリオ: {cs.scenario.name}</div>}
                    </td>
                    <td>
                      <div className="flex flex-wrap gap-1">
                        {cs.activities.map((a) => (
                          <Link
                            key={a.id}
                            to={`/activities/${a.id}`}
                            onClick={(e) => e.stopPropagation()}
                            className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-700 hover:bg-indigo-100"
                          >
                            {a.name}
                          </Link>
                        ))}
                        {cs.more_activities > 0 && <span className="text-xs text-slate-500">ほか {cs.more_activities} 件</span>}
                        {cs.activities.length === 0 && <span className="text-xs text-slate-400">—</span>}
                      </div>
                    </td>
                    <td className="text-xs text-slate-600">
                      {Object.entries(cs.tables)
                        .map(([t, n]) => `${tableLabels[t] ?? t} ${n}件`)
                        .join('、')}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
            {hasMore && (
              <div className="mt-3 text-center">
                <Button onClick={loadMore} disabled={loadingMore}>
                  {loadingMore ? '読み込み中…' : 'さらに表示'}
                </Button>
              </div>
            )}
          </>
        )}
      </Card>

      {selected && <ChangeSetDialog summary={selected} onClose={() => setSelected(null)} />}
    </>
  )
}

function Filter({ label, className, children }: { label: string; className?: string; children: ReactNode }) {
  return (
    <label className={`block text-xs font-medium text-slate-500 ${className ?? ''}`}>
      {label}
      <div className="mt-1">{children}</div>
    </label>
  )
}

// --- 詳細 ---

const actionLabels = { insert: '追加', update: '変更', delete: '削除' } as const
const actionTones = { insert: 'green', update: 'indigo', delete: 'red' } as const

function ChangeSetDialog({ summary, onClose }: { summary: ChangeSet; onClose: () => void }) {
  const { data, error } = useApi<{ change_set: ChangeSet; logs: ChangeLog[] }>(`/change-sets/${summary.id}`)
  return (
    <Dialog
      open
      wide
      title="変更の内容"
      onClose={onClose}
      footer={
        <Button variant="primary" onClick={onClose}>
          閉じる
        </Button>
      }
    >
      <dl className="mb-4 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1 text-sm">
        <dt className="text-slate-500">日時</dt>
        <dd>{formatDateTime(summary.created_at)}</dd>
        <dt className="text-slate-500">変更した人</dt>
        <dd>{summary.user.name}</dd>
        {summary.scenario && (
          <>
            <dt className="text-slate-500">シナリオ</dt>
            <dd>{summary.scenario.name}</dd>
          </>
        )}
        <dt className="text-slate-500">変更理由</dt>
        <dd className="whitespace-pre-wrap">{summary.reason || <Badge tone="amber">理由なし</Badge>}</dd>
      </dl>
      {error ? (
        <ErrorMessage error={error} />
      ) : !data ? (
        <Loading />
      ) : (
        <ul className="space-y-3">
          {data.logs.map((log) => (
            <li key={log.id} className="rounded-md border border-slate-200 p-3">
              <div className="mb-2 flex flex-wrap items-center gap-2">
                <Badge tone={actionTones[log.action]}>{actionLabels[log.action]}</Badge>
                <span className="text-xs text-slate-500">{tableLabels[log.table_name] ?? log.table_name}</span>
                <span className="text-sm font-medium text-slate-800">{log.label}</span>
              </div>
              <FieldChanges log={log} />
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  )
}

// 表示しない項目（ID や日時など、利用者に意味のないもの）
const hiddenFields = new Set(['id', 'created_at', 'updated_at', 'activity_id', 'scenario_id', 'activity_driver_id', 'created_by', 'level'])

const fieldLabels: Record<string, string> = {
  name: '名称',
  code: 'コード',
  amount: '金額',
  value: '値',
  probability: '確度',
  assumptions: '前提条件',
  start_date: '開始日',
  end_date: '終了日',
  status: 'ステータス',
  calc_mode: '算出方式',
  activity_type: 'タイプ',
  owner_user_id: '担当者',
  function_id: '機能',
  is_provisional: '仮の値',
  provisional_reason: '仮の値の理由',
  expression: '計算式',
  due_date: '期日',
  description: '内容',
  source: '登録方法',
  is_locked: 'ロック',
  parent_id: '親',
  sort_order: '表示順',
  role: 'ロール',
  is_active: '有効',
  email: 'メールアドレス',
  category: '区分',
  segment_id: 'セグメント',
  organization_id: '組織',
  target_month: '対象月',
  subject_id: '科目',
  driver_kind: '種別',
  unit: '単位',
  scenario_kind: '種別',
  fiscal_year: '年度',
  base_scenario_id: '複製元',
  password_changed: 'パスワード',
  copied: '複製した件数',
}

const valueLabels: Record<string, Record<string, string>> = {
  status: { ...activityStatusLabels, ...milestoneStatusLabels },
  calc_mode: calcModeLabels,
  activity_type: activityTypeLabels,
  driver_kind: driverKindLabels,
  role: roleLabels,
  category: categoryLabels,
  scenario_kind: scenarioKindLabels,
  source: { manual: '直接入力', formula: '計算式', import: '取込' },
}

function formatField(key: string, v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  if (typeof v === 'boolean') return key === 'password_changed' ? '変更' : v ? 'はい' : 'いいえ'
  if (key === 'amount') return `${formatYen(String(v))} 円`
  if (key === 'value') return formatNumber(String(v))
  if (key === 'probability') return formatPercent(v as number)
  if (valueLabels[key]?.[String(v)]) return valueLabels[key][String(v)]
  if (typeof v === 'object') {
    return Object.entries(v as Record<string, unknown>)
      .map(([k, n]) => `${tableLabels[k] ?? k} ${n}件`)
      .join('、')
  }
  return String(v)
}

/** 変更前後の項目の差分を表示する。追加・削除は内容をそのまま表示する */
function FieldChanges({ log }: { log: ChangeLog }) {
  const before = log.before ?? {}
  const after = log.after ?? {}
  const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])].filter((k) => !hiddenFields.has(k))
  const changed =
    log.action === 'update' ? keys.filter((k) => JSON.stringify(before[k]) !== JSON.stringify(after[k])) : keys.filter((k) => formatField(k, (log.action === 'insert' ? after : before)[k]) !== '—')

  if (changed.length === 0) return <p className="text-xs text-slate-500">項目の変更はありません</p>
  return (
    <table className="w-full text-sm">
      <tbody>
        {changed.map((k) => (
          <tr key={k} className="align-top">
            <th className="w-32 py-0.5 pr-3 text-left text-xs font-normal text-slate-500">{fieldLabels[k] ?? k}</th>
            {log.action === 'update' ? (
              <td className="py-0.5">
                <span className="text-slate-500 line-through decoration-slate-300">{formatField(k, before[k])}</span>
                <span className="mx-2 text-slate-400">→</span>
                <span className="font-medium text-slate-900">{formatField(k, after[k])}</span>
              </td>
            ) : (
              <td className={`py-0.5 whitespace-pre-wrap ${log.action === 'delete' ? 'text-slate-500 line-through' : ''}`}>{formatField(k, (log.action === 'insert' ? after : before)[k])}</td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
