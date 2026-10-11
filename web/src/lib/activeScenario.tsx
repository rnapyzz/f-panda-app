import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { ActiveScenarios, Scenario } from '../api/types'

type ActiveScenarioState = {
  /** 今回の見込（docs/plan.md「2.6」）。undefined は読み込み中、null は未設定 */
  active: Scenario | null | undefined
  /** 翌期の計画（12〜3月に作る翌年度の期初計画。docs/plan.md「2.25」）。なければ null */
  nextPlan: Scenario | null
  /** そのほかの作成中（締める前の前年度の版など）。シナリオ管理にだけ出す */
  others: Scenario[]
  /** シナリオ管理で変更したあとに呼ぶ */
  reload: () => Promise<void>
}

const ActiveScenarioContext = createContext<ActiveScenarioState>({ active: undefined, nextPlan: null, others: [], reload: async () => {} })

/** 作成中のシナリオ（年度ごとに1つ。今回の見込・翌期の計画）を、ログイン後の画面で共有する */
export function ActiveScenarioProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<Omit<ActiveScenarioState, 'reload'>>({ active: undefined, nextPlan: null, others: [] })
  const reload = useCallback(async () => {
    try {
      const res = await api.get<ActiveScenarios>('/scenarios/active')
      setState({ active: res.current, nextPlan: res.next_plan, others: res.others })
    } catch {
      setState({ active: null, nextPlan: null, others: [] })
    }
  }, [])
  useEffect(() => {
    reload()
  }, [reload])
  return <ActiveScenarioContext.Provider value={{ ...state, reload }}>{children}</ActiveScenarioContext.Provider>
}

export function useActiveScenario(): ActiveScenarioState {
  return useContext(ActiveScenarioContext)
}

/** 作成中の版の画面での呼び方（今回の見込・翌期の計画・作成中）。作成中でなければ空 */
export function activeLabel(s: Scenario, state: Pick<ActiveScenarioState, 'active' | 'nextPlan'>): string {
  if (state.active?.id === s.id) return '今回の見込'
  if (state.nextPlan?.id === s.id) return '翌期の計画'
  return s.is_active ? '作成中（締め待ち）' : ''
}
