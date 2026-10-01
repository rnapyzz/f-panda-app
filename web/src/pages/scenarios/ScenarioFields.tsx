import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import { planRoleLabels, type List, type PlanRole, type Scenario } from '../../api/types'
import { Button, Dialog, Field, FormError, Input, Select, Textarea, fieldError } from '../../components/ui'
import { yearMonthLabel } from '../../lib/format'
import { navigate } from '../../lib/router'
import { fiscalMonths } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

/** シナリオのエイリアス（期初計画・修正計画・最新見込）と決算確定月の入力欄 */
export function ScenarioRoleFields({
  scenarios,
  self,
  fiscalYear,
  planRole,
  actualThrough,
  onPlanRole,
  onActualThrough,
  throughDisabled,
  error,
}: {
  scenarios: Scenario[]
  self?: number
  fiscalYear: number
  planRole: PlanRole | ''
  actualThrough: string
  onPlanRole: (v: PlanRole | '') => void
  onActualThrough: (v: string) => void
  throughDisabled?: boolean
  error: unknown
}) {
  const holder = planRole ? scenarios.find((s) => s.fiscal_year === fiscalYear && s.plan_role === planRole && s.id !== self) : undefined
  return (
    <div className="grid grid-cols-2 gap-3">
      <Field
        label="エイリアス"
        error={fieldError(error, 'plan_role')}
        hint={holder ? `「${holder.name}」から付け替えます` : '年度ごとに1つのシナリオにだけ付けられます'}
      >
        {(p) => (
          <Select {...p} value={planRole} onChange={(e) => onPlanRole(e.target.value as PlanRole | '')}>
            <option value="">（なし）</option>
            {Object.entries(planRoleLabels).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </Select>
        )}
      </Field>
      <Field
        label="決算確定月"
        error={fieldError(error, 'actual_through')}
        hint={throughDisabled ? 'ロック中は変更できません' : 'この月以前は実績、それより後は計画値になります'}
      >
        {(p) => (
          <Select {...p} value={actualThrough} disabled={throughDisabled} onChange={(e) => onActualThrough(e.target.value)}>
            <option value="">（未設定: すべて計画値）</option>
            {fiscalMonths(fiscalYear).map((m) => (
              <option key={m} value={m}>
                {yearMonthLabel(m)}
              </option>
            ))}
          </Select>
        )}
      </Field>
    </div>
  )
}

/** シナリオの作成。作成したらシナリオの詳細を開く（onCreated を指定したときはそれを呼ぶ） */
export function CreateScenarioDialog({ scenarios, onClose, onCreated }: { scenarios: Scenario[]; onClose: () => void; onCreated?: (s: Scenario) => void }) {
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
      if (onCreated) onCreated(created)
      else navigate(`/scenarios/${created.id}`)
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

/** シナリオの設定（名称・エイリアス・決算確定月）の変更。決算確定月の変更は理由が必須 */
export function ScenarioSettingsDialog({ scenario, onClose, onSaved }: { scenario: Scenario; onClose: () => void; onSaved: (s: Scenario) => void }) {
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
