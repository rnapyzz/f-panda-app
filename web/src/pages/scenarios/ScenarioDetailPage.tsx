import { useMemo, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { activityTypeLabels, type Activity, type List, type PlanRole, type Scenario, type Unit } from '../../api/types'
import { useReason } from '../../components/ReasonDialog'
import { Button, Card, Dialog, Empty, ErrorMessage, Field, FormError, Input, Loading, PageHeader, Table, Textarea, fieldError } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { Link } from '../../lib/router'
import { useApi } from '../../lib/useApi'
import { ScenarioRoleFields } from './ScenarioFields'
import { ScenarioBadges } from './ScenarioListPage'

export function ScenarioDetailPage({ id }: { id: string }) {
  const me = useCurrentUser()
  const isAdmin = me.role === 'fpa_admin'
  const scenario = useApi<Scenario>(`/scenarios/${id}`)
  const activities = useApi<List<Activity>>('/activities')
  const units = useApi<List<Unit>>('/units')
  const unitName = useMemo(() => new Map((units.data?.items ?? []).map((f) => [f.id, f.name])), [units.data])
  const { askReason, dialog: reasonDialog } = useReason()
  const [renaming, setRenaming] = useState(false)
  const [q, setQ] = useState('')
  const [actionError, setActionError] = useState<unknown>(null)

  const error = scenario.error ?? activities.error ?? units.error
  if (error) return <ErrorMessage error={error} />
  if (!scenario.data || !activities.data || !units.data) return <Loading />
  const s = scenario.data

  const activate = async () => {
    setActionError(null)
    try {
      scenario.setData(await api.post<Scenario>(`/scenarios/${s.id}/activate`, {}))
    } catch (err) {
      setActionError(err)
    }
  }

  const setLocked = async (locked: boolean) => {
    setActionError(null)
    try {
      if (locked) {
        scenario.setData(await api.post<Scenario>(`/scenarios/${s.id}/lock`, {}))
      } else {
        await askReason('ロック解除の理由', async (reason) => {
          scenario.setData(await api.post<Scenario>(`/scenarios/${s.id}/unlock`, { reason }))
        })
      }
    } catch (err) {
      setActionError(err)
    }
  }

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
              <>
                {!s.is_active && !s.is_locked && (
                  <Button variant="primary" onClick={activate}>
                    作成中にする
                  </Button>
                )}
                <Button onClick={() => setRenaming(true)}>設定を変更</Button>
                {s.is_locked ? <Button onClick={() => setLocked(false)}>ロックを解除</Button> : <Button onClick={() => setLocked(true)}>🔒 ロックする</Button>}
              </>
            )}
          </>
        }
      />
      {actionError ? (
        <div className="mb-4">
          <ErrorMessage error={actionError} />
        </div>
      ) : null}
      {s.is_locked && <p className="mb-4 rounded-md bg-amber-50 px-4 py-2 text-sm text-amber-800">このシナリオはロックされています。数値は変更できません。</p>}
      {!s.is_locked && !s.is_active && (
        <p className="mb-4 rounded-md bg-slate-100 px-4 py-2 text-sm text-slate-700">このシナリオは作成中ではないため、数値を入力できるのは FP&A のみです。</p>
      )}

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
                    <Link to={`/scenarios/${s.id}/activities/${a.id}`} className="text-sm font-medium text-indigo-700 hover:underline">
                      {a.can_edit && !s.is_locked && (s.is_active || isAdmin) ? '数値を入力' : '数値を見る'} →
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {renaming && (
        <RenameDialog
          scenario={s}
          onClose={() => setRenaming(false)}
          onSaved={(updated) => {
            scenario.setData(updated)
            setRenaming(false)
          }}
        />
      )}
      {reasonDialog}
    </>
  )
}

/** シナリオの設定（名称・エイリアス・決算確定月）の変更。決算確定月の変更は理由が必須 */
function RenameDialog({ scenario, onClose, onSaved }: { scenario: Scenario; onClose: () => void; onSaved: (s: Scenario) => void }) {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const [name, setName] = useState(scenario.name)
  const [planRole, setPlanRole] = useState<PlanRole | ''>(scenario.plan_role ?? '')
  const [actualThrough, setActualThrough] = useState(scenario.actual_through ?? '')
  const [reason, setReason] = useState('')
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const throughChanged = actualThrough !== (scenario.actual_through ?? '')
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      onSaved(await api.put<Scenario>(`/scenarios/${scenario.id}`, { name, plan_role: planRole, actual_through: actualThrough, reason }))
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open
      title="シナリオの設定"
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button variant="primary" type="submit" form="rename-form" disabled={busy}>
            保存
          </Button>
        </>
      }
    >
      <form id="rename-form" onSubmit={submit} className="space-y-3">
        <Field label="シナリオ名" required error={fieldError(error, 'name')}>
          {(p) => <Input {...p} value={name} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <ScenarioRoleFields
          scenarios={scenarios.data?.items ?? []}
          self={scenario.id}
          fiscalYear={scenario.fiscal_year}
          planRole={planRole}
          actualThrough={actualThrough}
          onPlanRole={setPlanRole}
          onActualThrough={setActualThrough}
          throughDisabled={scenario.is_locked}
          error={error}
        />
        {throughChanged && (
          <Field label="変更理由" required error={fieldError(error, 'reason')} hint="決算確定月を変えると、表示する金額（実績と計画値の境目）が変わります">
            {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" placeholder="例: 2026-09 決算確定" />}
          </Field>
        )}
        <FormError error={error} fields={['name', 'plan_role', 'actual_through', 'reason']} />
      </form>
    </Dialog>
  )
}
