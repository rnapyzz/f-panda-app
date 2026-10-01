// シナリオの表示名と、比較の既定のシナリオ。

import { planRoleLabels, type PlanRole } from '../api/types.ts'

type ScenarioLike = { id: number; name: string; plan_role: PlanRole | null; is_active?: boolean; actual_through?: string | null }

/** 「名前（エイリアス）」。エイリアスがなければ名前だけ */
export function scenarioLabel(s: ScenarioLike): string {
  return s.plan_role ? `${s.name}（${planRoleLabels[s.plan_role]}）` : s.name
}

/** "2026-09" → "9月"。決算確定月の表示用 */
export function actualThroughLabel(ym: string | null | undefined): string {
  return ym ? `実績〜${Number(ym.slice(5, 7))}月` : '全月計画'
}

/**
 * 比較の既定のシナリオ（同じ年度のシナリオから選ぶ）。
 * 基準は修正計画、なければ期初計画、なければ最初に作られたシナリオ。
 * 最新は最新見込、なければ作成中、なければいちばん新しく作られたシナリオ（基準以外を優先）。
 */
export function defaultScenarios<S extends ScenarioLike>(inYear: S[]): { base?: S; latest?: S } {
  const byRole = (role: PlanRole) => inYear.find((s) => s.plan_role === role)
  const oldest = inYear.reduce<S | undefined>((a, s) => (!a || s.id < a.id ? s : a), undefined)
  const base = byRole('revised') ?? byRole('initial') ?? oldest
  const newest = inYear.filter((s) => s.id !== base?.id).reduce<S | undefined>((a, s) => (!a || s.id > a.id ? s : a), undefined)
  const latest = byRole('latest') ?? inYear.find((s) => s.is_active) ?? newest ?? base
  return { base, latest }
}

/** 年度（4月開始）の12か月（YYYY-MM） */
export function fiscalMonths(fiscalYear: number): string[] {
  return Array.from({ length: 12 }, (_, i) => {
    const m = 4 + i
    return m > 12 ? `${fiscalYear + 1}-${String(m - 12).padStart(2, '0')}` : `${fiscalYear}-${String(m).padStart(2, '0')}`
  })
}
