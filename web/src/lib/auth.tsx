import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, ApiError, onUnauthorized } from '../api/client'
import type { CurrentUser } from '../api/types'

type AuthState = {
  /** undefined は確認中、null は未ログイン */
  user: CurrentUser | null | undefined
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<CurrentUser | null | undefined>(undefined)

  useEffect(() => {
    api
      .get<{ user: CurrentUser }>('/auth/me')
      .then((res) => setUser(res.user))
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) setUser(null)
        else setUser(null)
      })
    return onUnauthorized(() => setUser(null))
  }, [])

  const login = useCallback(async (email: string, password: string) => {
    const res = await api.post<{ user: CurrentUser }>('/auth/login', { email, password })
    setUser(res.user)
  }, [])

  const logout = useCallback(async () => {
    await api.post('/auth/logout')
    setUser(null)
  }, [])

  return <AuthContext.Provider value={{ user, login, logout }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth は AuthProvider の中で使ってください')
  return ctx
}

/** ログイン中のユーザー。ログイン後の画面でのみ使う */
export function useCurrentUser(): CurrentUser {
  const { user } = useAuth()
  if (!user) throw new Error('ログインしていません')
  return user
}
