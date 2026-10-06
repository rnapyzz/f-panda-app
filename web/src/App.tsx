import type { ReactNode } from 'react'
import { Layout } from './components/Layout'
import { Loading, PageHeader } from './components/ui'
import { ActiveScenarioProvider } from './lib/activeScenario'
import { AuthProvider, useAuth } from './lib/auth'
import { matchPath, Redirect, useLocation } from './lib/router'
import { ActivityDetailPage } from './pages/activities/ActivityDetailPage'
import { HomePage } from './pages/HomePage'
import { ActivityListPage } from './pages/activities/ActivityListPage'
import { LoginPage } from './pages/LoginPage'
import { AllocationRulesPage } from './pages/masters/AllocationRulesPage'
import { ConfidenceLevelsPage } from './pages/masters/ConfidenceLevelsPage'
import { GLAccountsPage } from './pages/masters/GLAccountsPage'
import { UnitsPage } from './pages/masters/UnitsPage'
import { SubjectsPage } from './pages/masters/SubjectsPage'
import { TreeMasterPage } from './pages/masters/TreeMasterPage'
import { UsersPage } from './pages/masters/UsersPage'
import { HistoryPage } from './pages/history/HistoryPage'
import { ReportPage } from './pages/reports/ReportPage'
import { RiskPage } from './pages/reports/RiskPage'
import { ActualsPage } from './pages/scenarios/ActualsPage'
import { ScenarioAdminPage } from './pages/scenarios/ScenarioAdminPage'
import { ScenarioDetailPage } from './pages/scenarios/ScenarioDetailPage'
import { ScenarioListPage } from './pages/scenarios/ScenarioListPage'
import { ValuesPage } from './pages/scenarios/ValuesPage'

type Route = { path: string; render: (params: Record<string, string>) => ReactNode }

const routes: Route[] = [
  { path: '/', render: () => <HomePage /> },
  { path: '/activities', render: () => <ActivityListPage /> },
  { path: '/activities/:id', render: (p) => <ActivityDetailPage key={p.id} id={p.id} /> },
  { path: '/scenarios', render: () => <ScenarioListPage /> },
  { path: '/scenarios/:id', render: (p) => <ScenarioDetailPage key={p.id} id={p.id} /> },
  { path: '/scenarios/:sid/activities/:aid', render: (p) => <ValuesPage key={`${p.sid}/${p.aid}`} scenarioId={p.sid} activityId={p.aid} /> },
  { path: '/admin/scenarios', render: () => <ScenarioAdminPage /> },
  { path: '/admin/actuals', render: () => <ActualsPage /> },
  { path: '/reports', render: () => <ReportPage /> },
  { path: '/history', render: () => <HistoryPage /> },
  { path: '/risks', render: () => <RiskPage /> },
  { path: '/masters/organizations', render: () => <TreeMasterPage key="organizations" resource="organizations" /> },
  { path: '/masters/segments', render: () => <TreeMasterPage key="segments" resource="segments" /> },
  { path: '/masters/units', render: () => <UnitsPage /> },
  // 旧 URL（「機能」だった頃）
  { path: '/masters/functions', render: () => <Redirect to="/masters/units" /> },
  { path: '/masters/subjects', render: () => <SubjectsPage /> },
  { path: '/masters/gl-accounts', render: () => <GLAccountsPage /> },
  { path: '/masters/allocation-rules', render: () => <AllocationRulesPage /> },
  { path: '/masters/confidence-levels', render: () => <ConfidenceLevelsPage /> },
  { path: '/masters/users', render: () => <UsersPage /> },
]

export default function App() {
  return (
    <AuthProvider>
      <Screen />
    </AuthProvider>
  )
}

function Screen() {
  const { user } = useAuth()
  const { pathname } = useLocation()

  if (user === undefined) return <Loading />
  if (user === null) return <LoginPage />

  let page: ReactNode = <PageHeader title="ページが見つかりません" description="URL を確認してください。" />
  for (const r of routes) {
    const params = matchPath(r.path, pathname)
    if (params) {
      page = r.render(params)
      break
    }
  }
  return (
    <ActiveScenarioProvider>
      <Layout>{page}</Layout>
    </ActiveScenarioProvider>
  )
}
