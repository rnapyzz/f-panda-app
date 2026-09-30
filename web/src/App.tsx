import type { ReactNode } from 'react'
import { Layout } from './components/Layout'
import { Loading, PageHeader } from './components/ui'
import { AuthProvider, useAuth } from './lib/auth'
import { matchPath, Redirect, useLocation } from './lib/router'
import { ActivityDetailPage } from './pages/activities/ActivityDetailPage'
import { ActivityListPage } from './pages/activities/ActivityListPage'
import { LoginPage } from './pages/LoginPage'
import { FunctionsPage } from './pages/masters/FunctionsPage'
import { SubjectsPage } from './pages/masters/SubjectsPage'
import { TreeMasterPage } from './pages/masters/TreeMasterPage'
import { UsersPage } from './pages/masters/UsersPage'
import { ScenarioDetailPage } from './pages/scenarios/ScenarioDetailPage'
import { ScenarioListPage } from './pages/scenarios/ScenarioListPage'
import { ValuesPage } from './pages/scenarios/ValuesPage'

type Route = { path: string; render: (params: Record<string, string>) => ReactNode }

const routes: Route[] = [
  { path: '/', render: () => <Redirect to="/activities" /> },
  { path: '/activities', render: () => <ActivityListPage /> },
  { path: '/activities/:id', render: (p) => <ActivityDetailPage key={p.id} id={p.id} /> },
  { path: '/scenarios', render: () => <ScenarioListPage /> },
  { path: '/scenarios/:id', render: (p) => <ScenarioDetailPage key={p.id} id={p.id} /> },
  { path: '/scenarios/:sid/activities/:aid', render: (p) => <ValuesPage key={`${p.sid}/${p.aid}`} scenarioId={p.sid} activityId={p.aid} /> },
  { path: '/masters/organizations', render: () => <TreeMasterPage key="organizations" resource="organizations" /> },
  { path: '/masters/segments', render: () => <TreeMasterPage key="segments" resource="segments" /> },
  { path: '/masters/functions', render: () => <FunctionsPage /> },
  { path: '/masters/subjects', render: () => <SubjectsPage /> },
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

  for (const r of routes) {
    const params = matchPath(r.path, pathname)
    if (params) return <Layout>{r.render(params)}</Layout>
  }
  return (
    <Layout>
      <PageHeader title="ページが見つかりません" description="URL を確認してください。" />
    </Layout>
  )
}
