import { lazy, Suspense } from 'react'
import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ProtectedRoute } from './components/ProtectedRoute'
import { LoginPage } from './features/auth/LoginPage'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { DimensionsPage } from './features/dimensions/DimensionsPage'
import { ImportPage } from './features/import/ImportPage'
import { SheetsListPage } from './features/sheet/SheetsListPage'
import { VariancePage } from './features/variance/VariancePage'
import { VersionManagementPage } from './features/versions/VersionManagementPage'
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
    element: (
      <ProtectedRoute>
        <AppShell />
      </ProtectedRoute>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'dimensions', element: <DimensionsPage /> },
      { path: 'versions', element: <VersionManagementPage /> },
      { path: 'variance', element: <VariancePage /> },
      { path: 'sheets', element: <SheetsListPage /> },
      {
        path: 'sheets/:id',
        element: (
          <Suspense fallback={<p className={`${mutedText} m-8`}>読み込み中...</p>}>
            <SheetEditorPage />
          </Suspense>
        ),
      },
      { path: 'import', element: <ImportPage /> },
    ],
  },
])
