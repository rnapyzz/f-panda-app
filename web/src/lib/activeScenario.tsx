import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { Scenario } from '../api/types'

type ActiveScenarioState = {
  /** 作成中のシナリオ。undefined は読み込み中、null は未設定 */
  active: Scenario | null | undefined
  /** シナリオ管理で変更したあとに呼ぶ */
  reload: () => Promise<void>
}

const ActiveScenarioContext = createContext<ActiveScenarioState>({ active: undefined, reload: async () => {} })

/** 作成中のシナリオ（アプリ全体で1つ）を、ログイン後の画面で共有する */
export function ActiveScenarioProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<Scenario | null | undefined>(undefined)
  const reload = useCallback(async () => {
    try {
      setActive((await api.get<Scenario | null>('/scenarios/active')) ?? null)
    } catch {
      setActive(null)
    }
  }, [])
  useEffect(() => {
    reload()
  }, [reload])
  return <ActiveScenarioContext.Provider value={{ active, reload }}>{children}</ActiveScenarioContext.Provider>
}

export function useActiveScenario(): ActiveScenarioState {
  return useContext(ActiveScenarioContext)
}
