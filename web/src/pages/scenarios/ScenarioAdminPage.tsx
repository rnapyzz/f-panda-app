import { useState } from 'react'
import { api } from '../../api/client'
import { planRoleLabels, type FiscalYearClosing, type List, type Scenario, type ScenarioDrift } from '../../api/types'
import { useReason } from '../../components/ReasonDialog'
import { Button, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../../components/ui'
import { useActiveScenario } from '../../lib/activeScenario'
import { useCurrentUser } from '../../lib/auth'
import { formatDateTime, monthLabel, yearMonthLabel } from '../../lib/format'
import { defaultFiscalYear } from '../../lib/pl'
import { Link } from '../../lib/router'
import { currentFiscalYear, fiscalMonths, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { ActualsImportDialog } from './ActualsImportDialog'
import { RefreshActualsDialog } from './RefreshActualsDialog'
import { CreateScenarioDialog, ScenarioSettingsDialog } from './ScenarioFields'
import { ScenarioBadges } from './ScenarioListPage'

/**
 * シナリオ管理（FP&A のみ）。シナリオの作成、作成中の指定、エイリアス・決算確定月の設定、ロック、実績の取込を行う。
 */
export function ScenarioAdminPage() {
  const me = useCurrentUser()
  const scenarios = useApi<List<Scenario>>('/scenarios')
  if (me.role !== 'fpa_admin') {
    return <PageHeader title="シナリオ管理" description="この画面は FP&A のみが使えます。" />
  }
  if (scenarios.error) return <ErrorMessage error={scenarios.error} />
  if (!scenarios.data) return <Loading />
  return <AdminView scenarios={scenarios.data.items} reload={scenarios.reload} />
}

function AdminView({ scenarios, reload }: { scenarios: Scenario[]; reload: () => Promise<void> }) {
  const { active, reload: reloadActive } = useActiveScenario()
  const { askReason, dialog: reasonDialog } = useReason()
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const [currentYear] = useState(() => currentFiscalYear())
  const [fy, setFy] = useState(() => defaultFiscalYear(years) ?? currentYear)
  const yearOptions = [...new Set([...years, currentYear, fy])].sort((a, b) => b - a)
  const inYear = scenarios.filter((s) => s.fiscal_year === fy)

  const imported = useApi<{ months: string[] }>(`/actuals/months?fiscal_year=${fy}`)
  const importedMonths = new Set(imported.data?.months ?? [])
  const lastImported = imported.data?.months[imported.data.months.length - 1]

  // 締めた後の実績の修正（docs/plan.md「2.14」）: ロック済みのシナリオの食い違いと、年度の締め
  const drift = useApi<List<ScenarioDrift>>('/scenarios/actual-drift')
  const driftOf = new Map((drift.data?.items ?? []).map((d) => [d.scenario_id, d]))
  const closings = useApi<List<FiscalYearClosing>>('/fiscal-years/closings')
  const closing = closings.data?.items.find((c) => c.fiscal_year === fy)
  const [refreshing, setRefreshing] = useState<Scenario | null>(null)

  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)
  const [editing, setEditing] = useState<Scenario | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)

  const refresh = async () => {
    await Promise.all([reload(), reloadActive(), imported.reload(), drift.reload(), closings.reload()])
  }
  const run = async (op: () => Promise<unknown>) => {
    setActionError(null)
    try {
      await op()
      await refresh()
    } catch (err) {
      setActionError(err)
    }
  }
  const activate = (s: Scenario) => run(() => api.post(`/scenarios/${s.id}/activate`, {}))
  const lock = (s: Scenario) => run(() => api.post(`/scenarios/${s.id}/lock`, {}))
  const unlock = (s: Scenario) =>
    run(() =>
      askReason(`「${s.name}」のロック解除の理由`, async (reason) => {
        await api.post(`/scenarios/${s.id}/unlock`, { reason })
      }),
    )

  const closeYear = () =>
    run(() =>
      askReason(`${fy}年度を締める理由`, async (reason) => {
        await api.post(`/fiscal-years/${fy}/close`, { reason })
      }),
    )
  const reopenYear = () =>
    run(() =>
      askReason(`${fy}年度の締めを解除する理由`, async (reason) => {
        await api.post(`/fiscal-years/${fy}/reopen`, { reason })
      }),
    )

  return (
    <>
      <PageHeader
        title="シナリオ管理"
        description="作成中のシナリオ（アプリ全体での入力の対象）の指定、エイリアス・決算確定月の設定、ロック、実績の取込を行います。"
        actions={
          <>
            <Button onClick={() => setImporting(true)}>実績を取り込む</Button>
            <Button variant="primary" onClick={() => setCreating(true)}>
              ＋ シナリオを作成
            </Button>
          </>
        }
      />
      {actionError ? (
        <div className="mb-4">
          <ErrorMessage error={actionError} />
        </div>
      ) : null}

      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Card title="作成中のシナリオ">
          {active ? (
            <div className="space-y-1 text-sm">
              <Link to={`/scenarios/${active.id}`} className="font-medium text-indigo-700 hover:underline">
                {scenarioLabel(active)}
              </Link>
              <div>
                <ScenarioBadges s={active} />
              </div>
              <p className="text-xs text-slate-500">現場の担当者・マネージャーが数値を入力できるのは、このシナリオだけです。ロックすると作成中の指定は外れます。</p>
            </div>
          ) : (
            <p className="text-sm text-slate-500">作成中のシナリオはありません。下の一覧の「作成中にする」で指定してください。</p>
          )}
        </Card>
        <Card
          title="実績の取込状況"
          actions={
            <Select aria-label="年度" value={fy} onChange={(e) => setFy(Number(e.target.value))} className="w-28 py-1 text-xs">
              {yearOptions.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          }
        >
          <ul className="grid grid-cols-6 gap-1.5 text-center text-xs" aria-label="実績を取り込み済みの月">
            {fiscalMonths(fy).map((m) => (
              <li
                key={m}
                aria-label={`${yearMonthLabel(m)}: ${importedMonths.has(m) ? '取込済み' : '未取込'}`}
                className={cx('rounded border py-1.5', importedMonths.has(m) ? 'border-emerald-200 bg-emerald-50 font-medium text-emerald-800' : 'border-slate-200 text-slate-400')}
              >
                {monthLabel(m)}
              </li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-slate-500">{lastImported ? `${yearMonthLabel(lastImported)}まで取り込み済み。` : 'この年度の実績はまだありません。'}</p>
          <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-slate-100 pt-3 text-xs" aria-label="年度の締め">
            {closing ? (
              <>
                <span className="rounded bg-slate-100 px-1.5 py-0.5 font-medium text-slate-700">🔐 {fy}年度は締め済み</span>
                <span className="text-slate-500">
                  {formatDateTime(closing.closed_at)} {closing.closed_by_name}。この年度の実績は取り込み直せません。
                </span>
                <Button size="sm" variant="ghost" onClick={reopenYear}>
                  締めを解除
                </Button>
              </>
            ) : (
              <>
                <span className="text-slate-500">会計の決算が確定したら、年度を締めると実績の取り込み直しを止められます。</span>
                <Button size="sm" onClick={closeYear}>
                  {fy}年度を締める
                </Button>
              </>
            )}
          </div>
        </Card>
      </div>

      <Card title={`${fy}年度のシナリオ`}>
        {inYear.length === 0 ? (
          <Empty>この年度のシナリオはありません。「シナリオを作成」から追加してください。</Empty>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>シナリオ名</th>
                <th>エイリアス</th>
                <th>前回見込</th>
                <th>決算確定月</th>
                <th>状態</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {inYear.map((s) => (
                <tr key={s.id}>
                  <td>
                    <Link to={`/scenarios/${s.id}`} className="font-medium text-indigo-700 hover:underline">
                      {s.name}
                    </Link>
                  </td>
                  <td>{s.plan_role ? planRoleLabels[s.plan_role] : <span className="text-slate-400">—</span>}</td>
                  <td className="text-slate-600">{s.previous_scenario_id ? scenarios.find((x) => x.id === s.previous_scenario_id)?.name : <span className="text-slate-400">—</span>}</td>
                  <td className="whitespace-nowrap">
                    {s.actual_through ? yearMonthLabel(s.actual_through) : <span className="text-slate-400">未設定</span>}
                    {s.actual_through && !s.is_locked && (!lastImported || s.actual_through > lastImported) && (
                      <span className="ml-1 text-xs text-amber-700" title="決算確定月までの実績が取り込まれていません">
                        ⚠ 未取込の月あり
                      </span>
                    )}
                  </td>
                  <td>
                    <span className="inline-flex flex-wrap gap-1">
                      {s.is_active && <span className="text-xs font-medium text-emerald-700">✎ 作成中</span>}
                      {s.is_locked && <span className="text-xs font-medium text-amber-700">🔒 ロック済み</span>}
                      {driftOf.has(s.id) && (
                        <span className="text-xs font-medium text-red-700" title="保存した実績が、今の実績（会計側の修正を取り込んだもの）と違います">
                          ⚠ 実績の修正あり（{driftOf.get(s.id)!.months.length}か月）
                        </span>
                      )}
                      {!s.is_active && !s.is_locked && <span className="text-xs text-slate-400">—</span>}
                    </span>
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <Button size="sm" variant="ghost" onClick={() => setEditing(s)} aria-label={`${s.name}の設定`}>
                      設定
                    </Button>
                    {!s.is_active && !s.is_locked && (
                      <Button size="sm" variant="ghost" onClick={() => activate(s)} aria-label={`${s.name}を作成中にする`}>
                        作成中にする
                      </Button>
                    )}
                    {driftOf.has(s.id) && (
                      <Button size="sm" variant="ghost" onClick={() => setRefreshing(s)} aria-label={`${s.name}の実績を最新にする`}>
                        実績を最新にする
                      </Button>
                    )}
                    {s.is_locked ? (
                      <Button size="sm" variant="ghost" onClick={() => unlock(s)} aria-label={`${s.name}のロックを解除`}>
                        ロック解除
                      </Button>
                    ) : (
                      <Button size="sm" variant="ghost" onClick={() => lock(s)} aria-label={`${s.name}をロック`}>
                        🔒 ロック
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {creating && (
        <CreateScenarioDialog
          scenarios={scenarios}
          onClose={() => setCreating(false)}
          onCreated={async (created) => {
            setCreating(false)
            setFy(created.fiscal_year)
            await refresh()
          }}
        />
      )}
      {importing && <ActualsImportDialog onClose={() => setImporting(false)} onImported={refresh} />}
      {refreshing && <RefreshActualsDialog scenario={refreshing} onClose={() => setRefreshing(null)} onDone={refresh} />}
      {editing && (
        <ScenarioSettingsDialog
          scenario={editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null)
            await refresh()
          }}
        />
      )}
      {reasonDialog}
    </>
  )
}
