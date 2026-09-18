import { createContext, useCallback, useEffect, useState, type ReactNode } from 'react'
import { api, ApiError } from '../../api/client'
import type { CurrentUser } from './types'

interface AuthContextValue {
  user: CurrentUser | null
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

// eslint-disable-next-line react-refresh/only-export-components
export const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<CurrentUser | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api
      .get<CurrentUser>('/auth/me')
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const login = useCallback(async (email: string, password: string) => {
    const loggedInUser = await api.post<CurrentUser>('/auth/login', { email, password })
    setUser(loggedInUser)
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.post('/auth/logout')
    } catch (err) {
      if (!(err instanceof ApiError)) {
        throw err
      }
    }
    setUser(null)
  }, [])

  return (
    <AuthContext.Provider value={{ user, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}
