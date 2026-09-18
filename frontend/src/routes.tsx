import { lazy, Suspense } from 'react'
import { createBrowserRouter } from 'react-router-dom'
import { ProtectedRoute } from './components/ProtectedRoute'
import { LoginPage } from './features/auth/LoginPage'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { DimensionsPage } from './features/dimensions/DimensionsPage'
import { EntryPage } from './features/entry/EntryPage'
import { ImportPage } from './features/import/ImportPage'
import { SheetsListPage } from './features/sheet/SheetsListPage'
import { VariancePage } from './features/variance/VariancePage'
import { mutedText } from './lib/ui'

// Univer.js (and its formula-engine locale data) is multiple MB — code-split
// it so only the sheet editor route pays that cost, not every page load.
const SheetEditorPage = lazy(() => import('./features/sheet/SheetEditorPage').then((m) => ({ default: m.SheetEditorPage })))

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
  {
    path: '/sheets',
    element: (
      <ProtectedRoute>
        <SheetsListPage />
      </ProtectedRoute>
    ),
  },
  {
    path: '/sheets/:id',
    element: (
      <ProtectedRoute>
        <Suspense fallback={<p className={`${mutedText} m-8`}>読み込み中...</p>}>
          <SheetEditorPage />
        </Suspense>
      </ProtectedRoute>
    ),
  },
  {
    path: '/import',
    element: (
      <ProtectedRoute>
        <ImportPage />
      </ProtectedRoute>
    ),
  },
])
