// シナリオの表示名と、比較の既定のシナリオ。

import { planRoleLabels, type PlanRole } from '../api/types.ts'

type ScenarioLike = { id: number; name: string; plan_role: PlanRole | null; is_active?: boolean; actual_through?: string | null }

/** 「名前（エイリアス）」。エイリアスがなければ名前だけ */
export function scenarioLabel(s: ScenarioLike): string {
  return s.plan_role ? `${s.name}（${planRoleLabels[s.plan_role]}）` : s.name
}

/** "2026-09" → "9月"。決算確定月の表示用 */
export function actualThroughLabel(ym: string | null | undefined): string {
  return ym ? `${Number(ym.slice(5, 7))}月まで実績` : '全月計画'
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

/** 今日が属する年度（4月開始） */
export function currentFiscalYear(today = new Date()): number {
  return today.getMonth() >= 3 ? today.getFullYear() : today.getFullYear() - 1
}

/**
 * 締切の表示（docs/plan.md「2.13」）。today は日本時間の今日（YYYY-MM-DD）。
 * 3日以内は soon、過ぎたら overdue。
 */
export function deadlineStatus(deadline: string | null, today: string): { text: string; tone: 'normal' | 'soon' | 'overdue' } | null {
  if (!deadline) return null
  const [y, m, d] = deadline.split('-').map(Number)
  const days = Math.round((Date.UTC(y, m - 1, d) - Date.parse(`${today}T00:00:00Z`)) / 86_400_000)
  const date = `${m}/${d}`
  if (days < 0) return { text: `締切 ${date}（${-days}日過ぎています）`, tone: 'overdue' }
  if (days === 0) return { text: `締切 ${date}（今日）`, tone: 'soon' }
  return { text: `締切 ${date}（あと${days}日）`, tone: days <= 3 ? 'soon' : 'normal' }
}

/** 日本時間の今日（YYYY-MM-DD） */
export function todayInTokyo(now = new Date()): string {
  return new Date(now.getTime() + 9 * 3_600_000).toISOString().slice(0, 10)
}
