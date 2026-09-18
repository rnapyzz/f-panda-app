import { lazy, Suspense } from 'react'
import { createBrowserRouter } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { ProtectedRoute } from './components/ProtectedRoute'
import { AuditLogPage } from './features/admin/AuditLogPage'
import { SubmissionDashboardPage } from './features/admin/SubmissionDashboardPage'
import { UserAssignmentsPage } from './features/admin/UserAssignmentsPage'
import { ValidationReviewPage } from './features/admin/ValidationReviewPage'
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
    element: (
      <ProtectedRoute>
        <AppShell />
      </ProtectedRoute>
    ),
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'dimensions', element: <DimensionsPage /> },
      { path: 'entry', element: <EntryPage /> },
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
      { path: 'admin/assignments', element: <UserAssignmentsPage /> },
      { path: 'admin/submission-status', element: <SubmissionDashboardPage /> },
      { path: 'admin/validation', element: <ValidationReviewPage /> },
      { path: 'admin/audit-log', element: <AuditLogPage /> },
    ],
  },
])
