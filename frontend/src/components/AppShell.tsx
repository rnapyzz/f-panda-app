import { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import {
  BarChart3,
  ClipboardCheck,
  Database,
  LayoutDashboard,
  ListChecks,
  LogOut,
  Menu,
  PanelLeftClose,
  PanelLeftOpen,
  PencilLine,
  ScrollText,
  Table2,
  Upload,
  Users,
  X,
  type LucideIcon,
} from 'lucide-react'
import { useAuth } from '../features/auth/useAuth'
import { navLink, navLinkActive, pageContent } from '../lib/ui'

interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
}

const baseNavItems: NavItem[] = [
  { to: '/', label: 'ホーム', icon: LayoutDashboard, end: true },
  { to: '/dimensions', label: 'ディメンションマスタ管理', icon: Database },
  { to: '/entry', label: 'データ入力', icon: PencilLine },
  { to: '/sheets', label: 'マイシート', icon: Table2 },
  { to: '/variance', label: '予実差異レポート', icon: BarChart3 },
]

export function AppShell() {
  const { user, logout } = useAuth()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

  const items: NavItem[] =
    user?.role === 'office_admin'
      ? [
          ...baseNavItems,
          { to: '/import', label: '実績インポート', icon: Upload },
          { to: '/admin/assignments', label: '担当割当て管理', icon: Users },
          { to: '/admin/submission-status', label: '提出状況ダッシュボード', icon: ClipboardCheck },
          { to: '/admin/validation', label: 'バインディング検証結果', icon: ListChecks },
          { to: '/admin/audit-log', label: '監査ログ', icon: ScrollText },
        ]
      : baseNavItems

  return (
    <div className="flex h-screen overflow-hidden bg-white dark:bg-gray-900">
      {mobileOpen && <div className="fixed inset-0 z-30 bg-black/40 lg:hidden" onClick={() => setMobileOpen(false)} />}

      <aside
        className={`fixed inset-y-0 left-0 z-40 flex flex-col border-r border-sidebar-border bg-sidebar transition-transform duration-200 lg:static lg:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        } ${collapsed ? 'w-16' : 'w-64'}`}
      >
        <div className="flex h-14 items-center justify-between border-b border-sidebar-border px-3">
          {!collapsed && <span className="truncate text-sm font-bold text-gray-900 dark:text-gray-100">F-PANDA</span>}
          <button
            type="button"
            className="hidden rounded p-1.5 text-gray-500 hover:bg-gray-200 lg:inline-flex dark:text-gray-400 dark:hover:bg-gray-800"
            onClick={() => setCollapsed((c) => !c)}
            aria-label={collapsed ? 'サイドバーを展開' : 'サイドバーを折りたたむ'}
          >
            {collapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}
          </button>
          <button
            type="button"
            className="rounded p-1.5 text-gray-500 hover:bg-gray-200 lg:hidden dark:text-gray-400 dark:hover:bg-gray-800"
            onClick={() => setMobileOpen(false)}
            aria-label="メニューを閉じる"
          >
            <X size={18} />
          </button>
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto p-2">
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) => (isActive ? navLinkActive : navLink)}
              onClick={() => setMobileOpen(false)}
              title={collapsed ? item.label : undefined}
            >
              <item.icon size={18} className="shrink-0" />
              {!collapsed && <span className="truncate">{item.label}</span>}
            </NavLink>
          ))}
        </nav>

        <div className="border-t border-sidebar-border p-2">
          {!collapsed && user && (
            <div className="mb-1 px-2 text-xs">
              <p className="truncate font-medium text-gray-700 dark:text-gray-300">{user.name}</p>
              <p className="text-gray-500 dark:text-gray-400">{user.role === 'office_admin' ? '事務局管理者' : '現場担当者'}</p>
            </div>
          )}
          <button
            type="button"
            className={`${navLink} w-full`}
            onClick={() => void logout()}
            title={collapsed ? 'ログアウト' : undefined}
          >
            <LogOut size={18} className="shrink-0" />
            {!collapsed && <span>ログアウト</span>}
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center gap-2 border-b border-gray-200 px-4 lg:hidden dark:border-gray-700">
          <button
            type="button"
            className="rounded p-1.5 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
            onClick={() => setMobileOpen(true)}
            aria-label="メニューを開く"
          >
            <Menu size={20} />
          </button>
          <span className="text-sm font-bold text-gray-900 dark:text-gray-100">F-PANDA</span>
        </header>
        <main className="flex-1 overflow-y-auto">
          <div className={pageContent}>
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
