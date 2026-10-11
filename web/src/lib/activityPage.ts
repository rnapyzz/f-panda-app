// 施策の画面（docs/plan.md「2.19」）のタブ・シナリオの決め方。

import type { Milestone, Scenario } from '../api/types.ts'
import { currentFiscalYear, defaultScenarios } from './scenario.ts'

export type ActivityTab = 'update' | 'overview' | 'settings'

export const activityTabLabels: Record<ActivityTab, string> = { update: '今回の更新', overview: '概要', settings: '設定' }

const tabs = Object.keys(activityTabLabels) as ActivityTab[]

/** URL の tab。指定がなければ、施策を編集できて今回の見込があれば「今回の更新」、それ以外は「概要」 */
export function initialTab(param: string | null, canEdit: boolean, hasActive: boolean): ActivityTab {
  if (param && (tabs as string[]).includes(param)) return param as ActivityTab
  return canEdit && hasActive ? 'update' : 'overview'
}

/**
 * 表示するシナリオ。URL の scenario、なければ今回の見込（currentId）、なければ今年度の最新見込（なければ最初のシナリオ）。
 * 作成中は年度ごとに1つなので（docs/plan.md「2.25」）、作成中の版から1つを選ばず、今回の見込の ID を受け取る
 */
export function initialScenario(param: string | null, scenarios: Scenario[], currentId?: number | null, today = new Date()): Scenario | undefined {
  const byParam = scenarios.find((s) => String(s.id) === param)
  if (byParam) return byParam
  const current = scenarios.find((s) => s.id === currentId)
  if (current) return current
  const fy = currentFiscalYear(today)
  return defaultScenarios(scenarios.filter((s) => s.fiscal_year === fy)).latest ?? scenarios[0]
}

/** 期日を過ぎた（完了していない）・遅延のマイルストーン。「今回の更新」の上部で知らせる */
export function lateMilestones(milestones: Milestone[], today: string): Milestone[] {
  return milestones.filter((m) => m.status === 'delayed' || (m.status !== 'completed' && m.due_date < today))
}
