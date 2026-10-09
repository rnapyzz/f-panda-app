import { lazy, Suspense, useEffect, type ComponentType, type ReactNode } from 'react'
import { Layout } from './components/Layout'
import { Loading, PageHeader } from './components/ui'
import { ActiveScenarioProvider } from './lib/activeScenario'
import { AuthProvider, useAuth } from './lib/auth'
import { matchPath, Redirect, useLocation } from './lib/router'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'

/**
 * 画面は開いたときに読み込む（I-22）。最初に読み込むのは、ログインとホーム（ログイン後の最初の画面）とレイアウトだけ。
 * load は画面のモジュールを読み込む関数、name はモジュールの中の画面の名前。
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- 画面ごとに props が違うため
function page<M extends Record<K, ComponentType<any>>, K extends keyof M>(load: () => Promise<M>, name: K) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}

const loadActivityList = () => import('./pages/activities/ActivityListPage')
const loadActivityDetail = () => import('./pages/activities/ActivityDetailPage')
const ActivityListPage = page(loadActivityList, 'ActivityListPage')
const ActivityDetailPage = page(loadActivityDetail, 'ActivityDetailPage')
const ScenarioListPage = page(() => import('./pages/scenarios/ScenarioListPage'), 'ScenarioListPage')
const ScenarioDetailPage = page(() => import('./pages/scenarios/ScenarioDetailPage'), 'ScenarioDetailPage')
const ScenarioAdminPage = page(() => import('./pages/scenarios/ScenarioAdminPage'), 'ScenarioAdminPage')
const ActualsPage = page(() => import('./pages/scenarios/ActualsPage'), 'ActualsPage')
const NotificationSettingsPage = page(() => import('./pages/scenarios/NotificationSettingsPage'), 'NotificationSettingsPage')
const OrgChangesPage = page(() => import('./pages/scenarios/OrgChangesPage'), 'OrgChangesPage')
const ReportPage = page(() => import('./pages/reports/ReportPage'), 'ReportPage')
const RiskPage = page(() => import('./pages/reports/RiskPage'), 'RiskPage')
const HistoryPage = page(() => import('./pages/history/HistoryPage'), 'HistoryPage')
const TreeMasterPage = page(() => import('./pages/masters/TreeMasterPage'), 'TreeMasterPage')
const UnitsPage = page(() => import('./pages/masters/UnitsPage'), 'UnitsPage')
const SubjectsPage = page(() => import('./pages/masters/SubjectsPage'), 'SubjectsPage')
const GLAccountsPage = page(() => import('./pages/masters/GLAccountsPage'), 'GLAccountsPage')
const AllocationRulesPage = page(() => import('./pages/masters/AllocationRulesPage'), 'AllocationRulesPage')
const ConfidenceLevelsPage = page(() => import('./pages/masters/ConfidenceLevelsPage'), 'ConfidenceLevelsPage')
const UsersPage = page(() => import('./pages/masters/UsersPage'), 'UsersPage')
const ManualPage = page(() => import('./pages/manual/ManualPage'), 'ManualPage')

type Route = { path: string; render: (params: Record<string, string>) => ReactNode }

const routes: Route[] = [
  { path: '/manual', render: () => <ManualPage /> },
  { path: '/manual/:page', render: (p) => <ManualPage key={p.page} page={p.page} /> },
  { path: '/', render: () => <HomePage /> },
  { path: '/activities', render: () => <ActivityListPage /> },
  { path: '/activities/:id', render: (p) => <ActivityDetailPage key={p.id} id={p.id} /> },
  { path: '/scenarios', render: () => <ScenarioListPage /> },
  { path: '/scenarios/:id', render: (p) => <ScenarioDetailPage key={p.id} id={p.id} /> },
  // 旧 URL（数値入力の画面）は、施策の画面の「今回の更新」へ（docs/plan.md「2.19」）
  { path: '/scenarios/:sid/activities/:aid', render: (p) => <Redirect to={`/activities/${p.aid}?tab=update&scenario=${p.sid}`} /> },
  { path: '/admin/scenarios', render: () => <ScenarioAdminPage /> },
  { path: '/admin/actuals', render: () => <ActualsPage /> },
  { path: '/admin/notifications', render: () => <NotificationSettingsPage /> },
  { path: '/admin/org-changes', render: () => <OrgChangesPage /> },
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

  // ログインしたら、よく使う施策の画面を手が空いたときに読み込んでおき、開いたときに待たせない
  useEffect(() => {
    if (!user) return
    const t = window.setTimeout(() => {
      loadActivityList().catch(() => undefined)
      loadActivityDetail().catch(() => undefined)
    }, 1500)
    return () => window.clearTimeout(t)
  }, [user])

  if (user === undefined) return <Loading />
  if (user === null) return <LoginPage />

  let current: ReactNode = <PageHeader title="ページが見つかりません" description="URL を確認してください。" />
  for (const r of routes) {
    const params = matchPath(r.path, pathname)
    if (params) {
      current = r.render(params)
      break
    }
  }
  return (
    <ActiveScenarioProvider>
      <Layout>
        <Suspense fallback={<Loading />}>{current}</Suspense>
      </Layout>
    </ActiveScenarioProvider>
  )
}
