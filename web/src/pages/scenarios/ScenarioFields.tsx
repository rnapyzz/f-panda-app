import { planRoleLabels, type PlanRole, type Scenario } from '../../api/types'
import { Field, Select, fieldError } from '../../components/ui'
import { yearMonthLabel } from '../../lib/format'
import { fiscalMonths } from '../../lib/scenario'

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
