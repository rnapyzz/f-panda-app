import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { planRoleLabels, type ActivityDetail, type List, type PlanRole, type Scenario } from '../../api/types'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link, navigate, useLocation } from '../../lib/router'
import { actualThroughLabel } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'
import { ActualsImportDialog } from './ActualsImportDialog'
import { ScenarioRoleFields } from './ScenarioFields'

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
  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)

  const byId = new Map((scenarios.data?.items ?? []).map((s) => [s.id, s]))
  const target = (s: Scenario) => (activityId ? `/scenarios/${s.id}/activities/${activityId}` : `/scenarios/${s.id}`)

  return (
    <>
      <PageHeader
        title="シナリオ"
        description="計画・見込の版をシナリオとして管理します。決算確定月以前の月は実績、それより後の月は計画値です。見込は既存のシナリオを複製して作ります。"
        actions={
          canWrite && (
            <>
              <Button onClick={() => setImporting(true)}>実績を取り込む</Button>
              <Button variant="primary" onClick={() => setCreating(true)}>
                ＋ シナリオを作成
              </Button>
            </>
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
          <Empty>シナリオがありません{canWrite ? '。「シナリオを作成」から追加してください' : ''}</Empty>
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
      {creating && scenarios.data && <CreateScenarioDialog scenarios={scenarios.data.items} onClose={() => setCreating(false)} />}
      {importing && <ActualsImportDialog onClose={() => setImporting(false)} onImported={() => scenarios.reload()} />}
    </>
  )
}

function CreateScenarioDialog({ scenarios, onClose }: { scenarios: Scenario[]; onClose: () => void }) {
  const [name, setName] = useState('')
  const [planRole, setPlanRole] = useState<PlanRole | ''>('')
  const [actualThrough, setActualThrough] = useState('')
  // 初期値は今日が属する年度（4月開始）
  const [fiscalYear, setFiscalYear] = useState(() => {
    const now = new Date()
    return String(now.getMonth() + 1 >= 4 ? now.getFullYear() : now.getFullYear() - 1)
  })
  const [baseId, setBaseId] = useState('')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const candidates = scenarios.filter((s) => String(s.fiscal_year) === fiscalYear)

  // 決算確定月の既定値は、その年度で実績を取り込み済みの最終月
  useEffect(() => {
    const fy = Number(fiscalYear)
    if (fy < 2000 || fy > 2100) return
    let cancelled = false
    api
      .get<{ months: string[] }>(`/actuals/months?fiscal_year=${fy}`)
      .then((r) => !cancelled && setActualThrough(r.months[r.months.length - 1] ?? ''))
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [fiscalYear])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const created = await api.post<Scenario>('/scenarios', {
        name,
        fiscal_year: Number(fiscalYear),
        base_scenario_id: baseId ? Number(baseId) : null,
        plan_role: planRole,
        actual_through: actualThrough,
        reason,
      })
      navigate(`/scenarios/${created.id}`)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog
      open
      title="シナリオの作成"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="scenario-form" disabled={busy}>
            {busy ? '作成中…' : '作成'}
          </Button>
        </>
      }
    >
      <form id="scenario-form" onSubmit={submit} className="space-y-4">
        <Field label="シナリオ名" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} placeholder="例: 2026-10時点見込" />}
        </Field>
        <Field label="年度（4月開始）" required error={fieldError(error, 'fiscal_year')}>
          {(p) => (
            <Input
              {...p}
              type="number"
              value={fiscalYear}
              onChange={(e) => {
                setFiscalYear(e.target.value)
                setBaseId('')
              }}
              className="w-32"
            />
          )}
        </Field>
        <Field label="複製元のシナリオ" error={fieldError(error, 'base_scenario_id')} hint="選ぶと、ドライバー値・金額・想定条件をすべて引き継ぎます（同じ年度のみ）">
          {(p) => (
            <Select {...p} value={baseId} onChange={(e) => setBaseId(e.target.value)}>
              <option value="">（複製しない）</option>
              {candidates.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          )}
        </Field>
        <ScenarioRoleFields
          scenarios={scenarios}
          fiscalYear={Number(fiscalYear)}
          planRole={planRole}
          actualThrough={actualThrough}
          onPlanRole={setPlanRole}
          onActualThrough={setActualThrough}
          error={error}
        />
        <Field label="変更理由（任意）">{(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}</Field>
        <FormError error={error} fields={['name', 'fiscal_year', 'base_scenario_id', 'plan_role', 'actual_through']} />
      </form>
    </Dialog>
  )
}
