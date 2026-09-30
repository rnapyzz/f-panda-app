import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'

type State<T> = { data: T | undefined; error: Error | undefined; loading: boolean }

/** GET で取得し、reload で再取得できる。path が null のときは取得しない */
export function useApi<T>(path: string | null): State<T> & { reload: () => Promise<void>; setData: (d: T) => void } {
  const [state, setState] = useState<State<T>>({ data: undefined, error: undefined, loading: path !== null })

  const load = useCallback(async () => {
    if (path === null) return
    setState((s) => ({ ...s, loading: true }))
    try {
      const data = await api.get<T>(path)
      setState({ data, error: undefined, loading: false })
    } catch (error) {
      setState((s) => ({ ...s, error: error as Error, loading: false }))
    }
  }, [path])

  useEffect(() => {
    load()
  }, [load])

  const setData = useCallback((data: T) => setState({ data, error: undefined, loading: false }), [])
  return { ...state, reload: load, setData }
}
