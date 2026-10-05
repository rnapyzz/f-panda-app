import { useEffect, useState, type ReactNode } from 'react'
import { roleLabels } from '../api/types'
import { useActiveScenario } from '../lib/activeScenario'
import { useAuth, useCurrentUser } from '../lib/auth'
import { actualThroughLabel, scenarioLabel } from '../lib/scenario'
import { Link, useLocation } from '../lib/router'
import {
  IconActivities,
  IconAdmin,
  IconClose,
  IconCollapse,
  IconConfidence,
  IconHistory,
  IconHome,
  IconLogout,
  IconMenu,
  IconOrganizations,
  IconReports,
  IconRisks,
  IconScenarios,
  IconSegments,
  IconSubjects,
  IconUnits,
  IconUsers,
} from './icons'
import { cx } from './ui'

type NavItem = { to: string; label: string; icon: (p: { className?: string }) => ReactNode }
type NavGroup = { label: string; items: NavItem[]; adminOnly?: boolean }

const navGroups: NavGroup[] = [
  {
    label: '計画・入力',
    items: [
      { to: '/', label: 'ホーム', icon: IconHome },
      { to: '/activities', label: '施策', icon: IconActivities },
      { to: '/scenarios', label: 'シナリオ', icon: IconScenarios },
    ],
  },
  {
    label: '分析',
    items: [
      { to: '/reports', label: '予実比較', icon: IconReports },
      { to: '/risks', label: 'リスク', icon: IconRisks },
    ],
  },
  {
    label: '記録',
    items: [{ to: '/history', label: '変更履歴', icon: IconHistory }],
  },
  {
    label: 'マスタ',
    items: [
      { to: '/masters/organizations', label: '組織', icon: IconOrganizations },
      { to: '/masters/segments', label: 'セグメント', icon: IconSegments },
      { to: '/masters/units', label: 'ユニット', icon: IconUnits },
      { to: '/masters/subjects', label: '勘定科目', icon: IconSubjects },
      { to: '/masters/confidence-levels', label: '確度の段階', icon: IconConfidence },
      { to: '/masters/users', label: 'ユーザー', icon: IconUsers },
    ],
  },
  {
    label: '管理',
    adminOnly: true,
    items: [{ to: '/admin/scenarios', label: 'シナリオ管理', icon: IconAdmin }],
  },
]

const COLLAPSED_KEY = 'fpanda.sidebar.collapsed'

/** 折りたたみの状態はブラウザに保存する（保存できない環境では毎回広げた状態） */
function useCollapsed(): [boolean, (v: boolean) => void] {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSED_KEY) === '1'
    } catch {
      return false
    }
  })
  const update = (v: boolean) => {
    setCollapsed(v)
    try {
      localStorage.setItem(COLLAPSED_KEY, v ? '1' : '0')
    } catch {
      // 保存できなくても表示は切り替える
    }
  }
  return [collapsed, update]
}

export function Layout({ children }: { children: ReactNode }) {
  const [collapsed, setCollapsed] = useCollapsed()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const { pathname } = useLocation()

  // 画面を移動したら、狭い画面のメニューは閉じる
  const [lastPath, setLastPath] = useState(pathname)
  if (lastPath !== pathname) {
    setLastPath(pathname)
    setDrawerOpen(false)
  }

  useEffect(() => {
    if (!drawerOpen) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setDrawerOpen(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [drawerOpen])

  return (
    <div className="min-h-screen bg-slate-50 text-slate-800 md:flex">
      {/* 広い画面: 常に表示するサイドメニュー */}
      <aside
        className={cx(
          'sticky top-0 hidden h-screen shrink-0 border-r border-slate-200 bg-white transition-[width] duration-150 md:flex md:flex-col',
          collapsed ? 'w-14' : 'w-56',
        )}
      >
        <Sidebar collapsed={collapsed} onToggle={() => setCollapsed(!collapsed)} />
      </aside>

      {/* 狭い画面: 上部のバーと、開閉するメニュー */}
      <header className="sticky top-0 z-20 flex h-12 items-center gap-2 border-b border-slate-200 bg-white px-3 md:hidden">
        <button type="button" onClick={() => setDrawerOpen(true)} className="rounded p-1.5 text-slate-600 hover:bg-slate-100" aria-label="メニューを開く" aria-expanded={drawerOpen}>
          <IconMenu />
        </button>
        <Brand />
      </header>
      {drawerOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div className="absolute inset-0 bg-slate-900/40" onClick={() => setDrawerOpen(false)} aria-hidden="true" />
          <aside className="absolute inset-y-0 left-0 flex w-64 flex-col bg-white shadow-xl" aria-label="メニュー">
            <button type="button" onClick={() => setDrawerOpen(false)} className="absolute top-3 right-3 rounded p-1 text-slate-400 hover:bg-slate-100" aria-label="メニューを閉じる">
              <IconClose />
            </button>
            <Sidebar collapsed={false} />
          </aside>
        </div>
      )}

      <main className="min-w-0 flex-1">
        <ActiveScenarioBar />
        <div className="mx-auto max-w-[1440px] px-4 py-6 md:px-6">{children}</div>
      </main>
    </div>
  )
}

/** 作成中のシナリオ（アプリ全体での入力の対象）を、すべての画面の上部に表示する */
function ActiveScenarioBar() {
  const { active } = useActiveScenario()
  const user = useCurrentUser()
  if (active === undefined) return null
  return (
    <div className="border-b border-emerald-100 bg-emerald-50/70 px-4 py-1.5 text-xs text-emerald-900 md:px-6" role="status" aria-label="作成中のシナリオ">
      <div className="mx-auto flex max-w-[1440px] flex-wrap items-center gap-x-2 gap-y-1">
        <span className="font-semibold">✎ 作成中</span>
        {active ? (
          <>
            <Link to={`/scenarios/${active.id}`} className="font-medium underline-offset-2 hover:underline">
              {scenarioLabel(active)}
            </Link>
            <span className="text-emerald-700">
              {active.fiscal_year}年度・{actualThroughLabel(active.actual_through)}
            </span>
          </>
        ) : (
          <span className="text-emerald-700">
            なし（数値を入力できるのは FP&A のみです）
            {user.role === 'fpa_admin' && (
              <Link to="/admin/scenarios" className="ml-2 font-medium underline">
                シナリオ管理で指定する
              </Link>
            )}
          </span>
        )}
      </div>
    </div>
  )
}

function Brand({ compact }: { compact?: boolean }) {
  return (
    <Link to="/" className="flex shrink-0 items-center gap-2 text-base font-bold whitespace-nowrap text-indigo-700" aria-label="F-Panda（ホーム）">
      <img src="/favicon.svg" alt="" width={28} height={28} />
      {!compact && 'F-Panda'}
    </Link>
  )
}

function Sidebar({ collapsed, onToggle }: { collapsed: boolean; onToggle?: () => void }) {
  const user = useCurrentUser()
  const { logout } = useAuth()
  const { pathname } = useLocation()
  const isActive = (to: string) => pathname === to || pathname.startsWith(to + '/')

  return (
    <>
      {/* 折りたたみボタンは、ログアウトと離して上部に置く */}
      <div className={cx('flex shrink-0 items-center border-b border-slate-100', collapsed ? 'flex-col gap-2 py-3' : 'h-14 justify-between pr-2 pl-4')}>
        <Brand compact={collapsed} />
        {onToggle && (
          <button
            type="button"
            onClick={onToggle}
            className="rounded-md p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
            aria-label={collapsed ? 'メニューを広げる' : 'メニューを折りたたむ'}
            title={collapsed ? 'メニューを広げる' : 'メニューを折りたたむ'}
          >
            <IconCollapse collapsed={collapsed} />
          </button>
        )}
      </div>

      <nav className="flex-1 overflow-y-auto py-3" aria-label="メインメニュー">
        {navGroups.filter((g) => !g.adminOnly || user.role === 'fpa_admin').map((g) => (
          <div key={g.label} className="mb-3">
            {collapsed ? (
              <div className="mx-3 mb-2 border-t border-slate-100" aria-hidden="true" />
            ) : (
              <div className="px-4 pb-1 text-[11px] font-semibold tracking-wide text-slate-400">{g.label}</div>
            )}
            <ul className="space-y-0.5 px-2">
              {g.items.map((item) => {
                const active = isActive(item.to)
                const Icon = item.icon
                return (
                  <li key={item.to}>
                    <Link
                      to={item.to}
                      aria-current={active ? 'page' : undefined}
                      aria-label={collapsed ? item.label : undefined}
                      title={collapsed ? item.label : undefined}
                      className={cx(
                        'flex items-center gap-3 rounded-md py-1.5 text-sm font-medium whitespace-nowrap',
                        collapsed ? 'justify-center px-0' : 'px-2.5',
                        active ? 'bg-indigo-50 text-indigo-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900',
                      )}
                    >
                      <Icon className={cx('size-5 shrink-0', active ? 'text-indigo-600' : 'text-slate-400')} />
                      {!collapsed && item.label}
                    </Link>
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </nav>

      <div className={cx('shrink-0 border-t border-slate-100 py-2', collapsed ? 'px-2' : 'px-3')}>
        {!collapsed && (
          <div className="mb-1 px-1.5 text-sm">
            <div className="truncate font-medium text-slate-700">{user.name}</div>
            <div className="text-xs text-slate-400">{roleLabels[user.role]}</div>
          </div>
        )}
        <button
          type="button"
          onClick={() => logout()}
          title={collapsed ? `ログアウト（${user.name}）` : undefined}
          className={cx('flex w-full items-center gap-2 rounded-md py-1.5 text-sm text-slate-500 hover:bg-slate-100 hover:text-slate-800', collapsed ? 'justify-center' : 'px-1.5')}
        >
          <IconLogout className="size-5 shrink-0" />
          {collapsed ? <span className="sr-only">ログアウト</span> : 'ログアウト'}
        </button>
      </div>
    </>
  )
}
