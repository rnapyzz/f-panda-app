import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { scenarioKindLabels, type ActivityDetail, type List, type Scenario, type ScenarioKind } from '../../api/types'
import { Badge, Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Select, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link, navigate, useLocation } from '../../lib/router'
import { useApi } from '../../lib/useApi'

export const kindTone: Record<ScenarioKind, 'slate' | 'indigo' | 'green' | 'amber' | 'red'> = {
  budget: 'indigo',
  forecast: 'green',
  actual: 'slate',
  optimistic: 'amber',
  pessimistic: 'red',
  other: 'slate',
}

export function ScenarioBadges({ s }: { s: Scenario }) {
  return (
    <>
      <Badge tone={kindTone[s.scenario_kind]}>{scenarioKindLabels[s.scenario_kind]}</Badge>
      {s.is_locked && <Badge tone="amber">🔒 ロック済み</Badge>}
    </>
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

  const byId = new Map((scenarios.data?.items ?? []).map((s) => [s.id, s]))
  const target = (s: Scenario) => (activityId ? `/scenarios/${s.id}/activities/${activityId}` : `/scenarios/${s.id}`)

  return (
    <>
      <PageHeader
        title="シナリオ"
        description="予算・見込の版や楽観/悲観、実績をシナリオとして管理します。見込は既存のシナリオを複製して作ります。"
        actions={canWrite && <Button variant="primary" onClick={() => setCreating(true)}>＋ シナリオを作成</Button>}
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
                <th>種別・状態</th>
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
                  <td className="space-x-1">
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
    </>
  )
}

function CreateScenarioDialog({ scenarios, onClose }: { scenarios: Scenario[]; onClose: () => void }) {
  const [name, setName] = useState('')
  const [kind, setKind] = useState<ScenarioKind>('forecast')
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

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const created = await api.post<Scenario>('/scenarios', {
        name,
        scenario_kind: kind,
        fiscal_year: Number(fiscalYear),
        base_scenario_id: baseId && kind !== 'actual' ? Number(baseId) : null,
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
        <div className="grid grid-cols-2 gap-3">
          <Field label="種別" required error={fieldError(error, 'scenario_kind')}>
            {(p) => (
              <Select {...p} value={kind} onChange={(e) => setKind(e.target.value as ScenarioKind)}>
                {Object.entries(scenarioKindLabels).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </Select>
            )}
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
              />
            )}
          </Field>
        </div>
        {kind !== 'actual' && (
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
        )}
        {kind === 'actual' && <p className="text-xs text-slate-500">実績シナリオには、会計データの CSV を取り込みます。</p>}
        <Field label="変更理由（任意）">{(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}</Field>
        <FormError error={error} fields={['name', 'scenario_kind', 'fiscal_year', 'base_scenario_id']} />
      </form>
    </Dialog>
  )
}
