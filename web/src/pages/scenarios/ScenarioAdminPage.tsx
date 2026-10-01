import { useState } from 'react'
import { api } from '../../api/client'
import { planRoleLabels, type List, type Scenario } from '../../api/types'
import { useReason } from '../../components/ReasonDialog'
import { Button, Card, Empty, ErrorMessage, Loading, PageHeader, Select, Table, cx } from '../../components/ui'
import { useActiveScenario } from '../../lib/activeScenario'
import { useCurrentUser } from '../../lib/auth'
import { monthLabel, yearMonthLabel } from '../../lib/format'
import { defaultFiscalYear } from '../../lib/pl'
import { Link } from '../../lib/router'
import { currentFiscalYear, fiscalMonths, scenarioLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { ActualsImportDialog } from './ActualsImportDialog'
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

  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)
  const [editing, setEditing] = useState<Scenario | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)

  const refresh = async () => {
    await Promise.all([reload(), reloadActive(), imported.reload()])
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
      {importing && <ActualsImportDialog onClose={() => setImporting(false)} onImported={() => imported.reload()} />}
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
