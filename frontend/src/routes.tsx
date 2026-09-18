import { createBrowserRouter } from 'react-router-dom'
import { ProtectedRoute } from './components/ProtectedRoute'
import { LoginPage } from './features/auth/LoginPage'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { DimensionsPage } from './features/dimensions/DimensionsPage'
import { EntryPage } from './features/entry/EntryPage'
import { VariancePage } from './features/variance/VariancePage'

export const router = createBrowserRouter([
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    path: '/',
    element: (
      <ProtectedRoute>
        <DashboardPage />
      </ProtectedRoute>
    ),
  },
  {
    path: '/dimensions',
    element: (
      <ProtectedRoute>
        <DimensionsPage />
      </ProtectedRoute>
    ),
  },
  {
    path: '/entry',
    element: (
      <ProtectedRoute>
        <EntryPage />
      </ProtectedRoute>
    ),
  },
  {
    path: '/variance',
    element: (
      <ProtectedRoute>
        <VariancePage />
      </ProtectedRoute>
    ),
  },
])
