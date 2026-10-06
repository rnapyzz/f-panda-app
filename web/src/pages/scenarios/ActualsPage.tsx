import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { Activity, List, ReallocateResult, UnallocatedGroup, UnallocatedList } from '../../api/types'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Loading, PageHeader, Select, Table, Textarea, cx, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { formatYen, monthLabel, yearMonthLabel } from '../../lib/format'
import { Link } from '../../lib/router'
import { currentFiscalYear, fiscalMonths } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { ActivitySelect } from '../masters/AllocationRulesPage'
import { ActualsImportDialog } from './ActualsImportDialog'

/**
 * 実績の割当（FP&A のみ、docs/plan.md「2.12」）。実績の取込、未割当の一覧からの割当、過去の月の再割当を行う。
 */
export function ActualsPage() {
  const me = useCurrentUser()
  if (me.role !== 'fpa_admin') {
    return <PageHeader title="実績の割当" description="この画面は FP&A のみが使えます。" />
  }
  return <ActualsView />
}

function ActualsView() {
  const [fy, setFy] = useState(() => currentFiscalYear())
  const unallocated = useApi<UnallocatedList>(`/actuals/unallocated?fiscal_year=${fy}`)
  const imported = useApi<{ months: string[] }>(`/actuals/months?fiscal_year=${fy}`)
  const activities = useApi<List<Activity>>('/activities')
  const [importing, setImporting] = useState(false)
  const [assigning, setAssigning] = useState<UnallocatedGroup | null>(null)
  const [reallocating, setReallocating] = useState(false)
  const years = [fy + 1, fy, fy - 1, fy - 2].filter((y, i, a) => a.indexOf(y) === i)
  const refresh = () => Promise.all([unallocated.reload(), imported.reload()])

  const list = unallocated.data
  return (
    <>
      <PageHeader
        title="実績の割当"
        description={
          <>
            会計の明細を取り込み、施策に割り当てます。箱の ID（施策コード・外部コード）→ <Link to="/masters/allocation-rules" className="text-indigo-700 hover:underline">割当ルール</Link>（会計科目 × 部門）の順に割り当て、どちらにも当たらない行は未割当になります。
            未割当も実績として取り込むため、アプリの合計は会計の合計と一致します。
          </>
        }
        actions={
          <>
            <Button onClick={() => setReallocating(true)}>再割当</Button>
            <Button variant="primary" onClick={() => setImporting(true)}>
              実績を取り込む
            </Button>
          </>
        }
      />

      <Card
        title={
          <span className="flex flex-wrap items-center gap-3">
            未割当の実績
            {list && (
              <span className="text-xs font-normal text-slate-500" aria-label="未割当の合計">
                {list.count} 行・収益 {formatYen(list.revenue)} ／ 費用 {formatYen(list.expense)}
              </span>
            )}
          </span>
        }
        actions={
          <div className="w-32">
            <Select aria-label="年度" value={fy} onChange={(e) => setFy(Number(e.target.value))} className="py-1 text-xs">
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          </div>
        }
      >
        {unallocated.error ? (
          <ErrorMessage error={unallocated.error} />
        ) : !list ? (
          <Loading />
        ) : list.items.length === 0 ? (
          <Empty>未割当の実績はありません。すべての実績が施策に割り当てられています。</Empty>
        ) : (
          <>
            <p className="mb-2 text-xs text-slate-500">
              施策を選ぶと、翌月以降も同じ施策に入るように残します（箱の ID は施策の外部コードとして登録、箱の ID がないものは割当ルールを追加）。金額の大きい順に並べています。
            </p>
            <Table>
              <thead>
                <tr>
                  <th>まとまり</th>
                  <th>会計科目</th>
                  <th>月</th>
                  <th className="text-right">行数</th>
                  <th className="text-right">収益</th>
                  <th className="text-right">費用</th>
                  <th>摘要の例</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {list.items.map((g) => (
                  <tr key={g.key}>
                    <td className="whitespace-nowrap">
                      {g.box_code !== null ? (
                        <>
                          <Badge tone="indigo">箱の ID</Badge> <span className="font-mono text-sm">{g.box_code}</span>
                        </>
                      ) : (
                        <>
                          <Badge tone="slate">部門</Badge> <span className="font-mono text-sm">{g.department_code ?? '（なし）'}</span>
                        </>
                      )}
                    </td>
                    <td className="text-sm">{g.accounts.join('、')}</td>
                    <td className="text-xs whitespace-nowrap text-slate-600">{g.months.map(monthLabel).join('・')}</td>
                    <td className="text-right tabular-nums">{g.count}</td>
                    <td className="text-right tabular-nums">{g.revenue === '0' ? <span className="text-slate-300">—</span> : formatYen(g.revenue)}</td>
                    <td className="text-right tabular-nums">{g.expense === '0' ? <span className="text-slate-300">—</span> : formatYen(g.expense)}</td>
                    <td className="max-w-48 truncate text-xs text-slate-500" title={g.description}>
                      {g.description}
                    </td>
                    <td className="text-right">
                      <Button size="sm" onClick={() => setAssigning(g)}>
                        施策を選ぶ
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </>
        )}
      </Card>

      {importing && <ActualsImportDialog onClose={() => setImporting(false)} onImported={refresh} />}
      {assigning && activities.data && (
        <AssignDialog
          group={assigning}
          activities={activities.data.items}
          onClose={() => setAssigning(null)}
          onAssigned={async () => {
            setAssigning(null)
            await refresh()
          }}
        />
      )}
      {reallocating && <ReallocateDialog fiscalYear={fy} importedMonths={imported.data?.months ?? []} onClose={() => setReallocating(false)} onDone={refresh} />}
    </>
  )
}

function AssignDialog({ group, activities, onClose, onAssigned }: { group: UnallocatedGroup; activities: Activity[]; onClose: () => void; onAssigned: () => Promise<void> }) {
  const [activityId, setActivityId] = useState('')
  const [allDepartments, setAllDepartments] = useState(false)
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const byBox = group.box_code !== null

  const submit = async () => {
    setBusy(true)
    setError(null)
    const target = byBox
      ? { box_code: group.box_code }
      : { gl_account_id: group.gl_account_id, department_code: group.department_code, all_departments: allDepartments }
    try {
      await api.post('/actuals/unallocated/assign', { ...target, activity_id: Number(activityId) || 0, reason })
      await onAssigned()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title="未割当の実績を施策に割り当てる"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" disabled={busy || !activityId || !reason.trim()} onClick={submit}>
            {busy ? '処理中…' : '割り当てる'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <dl className="grid grid-cols-[5rem_1fr] gap-x-3 gap-y-1 rounded-md bg-slate-50 p-3 text-sm">
          {byBox ? (
            <>
              <dt className="text-slate-500">箱の ID</dt>
              <dd className="font-mono">{group.box_code}</dd>
            </>
          ) : (
            <>
              <dt className="text-slate-500">部門</dt>
              <dd className="font-mono">{group.department_code ?? '（なし）'}</dd>
            </>
          )}
          <dt className="text-slate-500">会計科目</dt>
          <dd>{group.accounts.join('、')}</dd>
          <dt className="text-slate-500">対象</dt>
          <dd>
            {group.count} 行（{group.months.map(monthLabel).join('・')}）
          </dd>
        </dl>
        <Field label="割り当てる施策" required error={fieldError(error, 'activity_id')}>
          {(p) => <ActivitySelect {...p} activities={activities} value={activityId} onChange={setActivityId} />}
        </Field>
        <p className="text-xs text-slate-600">
          {byBox
            ? `箱の ID「${group.box_code}」を、選んだ施策の外部コードとして登録します。翌月以降もこの ID の実績は同じ施策に入ります。`
            : group.department_code
              ? '会計科目 × 部門の割当ルールを追加します。翌月以降も同じ会計科目・部門の実績は同じ施策に入ります。'
              : '部門のない実績なので、この会計科目の全部門に当たる割当ルールを追加します。'}
        </p>
        {!byBox && group.department_code && (
          <label className="flex items-center gap-2 text-sm text-slate-700">
            <input type="checkbox" className="size-4 rounded border-slate-300" checked={allDepartments} onChange={(e) => setAllDepartments(e.target.checked)} />
            部門を問わず、この会計科目はすべてこの施策に割り当てる（全部門のルール）
          </label>
        )}
        <Field label="変更理由" required error={fieldError(error, 'reason')} hint="例: 新規案件 P-9001 を受託開発に割当">
          {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
        </Field>
        <FormError error={error} fields={['activity_id', 'reason']} />
      </div>
    </Dialog>
  )
}

function ReallocateDialog({ fiscalYear, importedMonths, onClose, onDone }: { fiscalYear: number; importedMonths: string[]; onClose: () => void; onDone: () => Promise<unknown> }) {
  const [months, setMonths] = useState<string[]>([])
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<ReallocateResult | null>(null)
  const [done, setDone] = useState<ReallocateResult | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const imported = new Set(importedMonths)

  const send = async (dryRun: boolean) => {
    setBusy(true)
    setError(null)
    try {
      const res = await api.post<ReallocateResult>('/actuals/reallocate', { months, reason, dry_run: dryRun })
      if (dryRun) setPreview(res)
      else {
        setDone(res)
        await onDone()
      }
    } catch (err) {
      setError(err)
      setPreview(null)
    } finally {
      setBusy(false)
    }
  }
  const toggle = (m: string) => {
    setPreview(null)
    setMonths((prev) => (prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m].sort()))
  }
  const result = done ?? preview

  return (
    <Dialog
      open
      wide
      title="再割当"
      onClose={onClose}
      footer={
        done ? (
          <Button variant="primary" onClick={onClose}>
            閉じる
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>キャンセル</Button>
            <Button disabled={busy || months.length === 0} onClick={() => send(true)}>
              内容を確認
            </Button>
            <Button variant="primary" disabled={busy || !preview || !reason.trim()} onClick={() => send(false)}>
              {busy ? '処理中…' : '再割当する'}
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        {!done && (
          <>
            <p className="text-sm text-slate-600">
              取込済みの明細に、今の外部コード・割当ルール・会計科目の対応を当て直します。ロック済みのシナリオの実績は変わりません。
            </p>
            <fieldset>
              <legend className="mb-1 text-sm font-medium text-slate-700">再割当する月（{fiscalYear}年度）</legend>
              <div className="grid grid-cols-6 gap-1.5">
                {fiscalMonths(fiscalYear).map((m) => (
                  <label
                    key={m}
                    className={cx(
                      'flex items-center justify-center gap-1 rounded border px-1 py-1 text-xs',
                      imported.has(m) ? 'cursor-pointer border-slate-200' : 'border-slate-100 text-slate-300',
                      months.includes(m) && 'border-indigo-400 bg-indigo-50 text-indigo-800',
                    )}
                  >
                    <input type="checkbox" className="size-3.5" disabled={!imported.has(m)} checked={months.includes(m)} onChange={() => toggle(m)} />
                    {monthLabel(m)}
                  </label>
                ))}
              </div>
            </fieldset>
            <Field label="変更理由" required error={fieldError(error, 'reason')} hint="例: 給与の受け皿を開発部に変更">
              {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
            </Field>
          </>
        )}
        {error && !(error instanceof ApiError && error.details.reason) ? <ErrorMessage error={error} /> : null}
        {result && (
          <div className="space-y-2">
            <p className={done ? 'rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-800' : 'text-sm text-slate-700'}>
              {done ? '再割当しました。' : '再割当すると、次のように変わります（まだ保存していません）。'}
              {result.months.map(yearMonthLabel).join('・')} の明細 {result.entries} 行のうち、割当が変わる行 {result.changed} 行
              {result.removed > 0 && `、対象外になった会計科目の行 ${result.removed} 行（取り除きます）`}
            </p>
            {result.changes.length === 0 ? (
              <p className="text-sm text-slate-500">施策ごとの金額は変わりません。</p>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <th>施策</th>
                    <th className="text-right">収益の増減</th>
                    <th className="text-right">費用の増減</th>
                  </tr>
                </thead>
                <tbody>
                  {result.changes.map((c) => (
                    <tr key={c.activity?.id ?? 0}>
                      <td>{c.activity ? `${c.activity.name}（${c.activity.code}）` : <span className="text-amber-700">未割当</span>}</td>
                      <td className="text-right tabular-nums">{signed(c.revenue)}</td>
                      <td className="text-right tabular-nums">{signed(c.expense)}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}

function signed(v: string): string {
  if (v === '0') return '—'
  return `${v.startsWith('-') ? '' : '+'}${formatYen(v)}`
}
