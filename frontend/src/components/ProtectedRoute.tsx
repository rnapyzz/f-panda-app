import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../features/auth/useAuth'

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth()

  if (loading) {
    return <p>読み込み中...</p>
  }
  if (!user) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}
