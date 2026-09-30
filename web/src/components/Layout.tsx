import type { ReactNode } from 'react'
import { roleLabels } from '../api/types'
import { useAuth, useCurrentUser } from '../lib/auth'
import { Link, useLocation } from '../lib/router'
import { cx } from './ui'

type NavItem = { to: string; label: string }

const mainNav: NavItem[] = [
  { to: '/activities', label: '施策' },
  { to: '/scenarios', label: 'シナリオ' },
]

const masterNav: NavItem[] = [
  { to: '/masters/organizations', label: '組織' },
  { to: '/masters/segments', label: 'セグメント' },
  { to: '/masters/functions', label: '機能' },
  { to: '/masters/subjects', label: '勘定科目' },
  { to: '/masters/users', label: 'ユーザー' },
]

export function Layout({ children }: { children: ReactNode }) {
  const user = useCurrentUser()
  const { logout } = useAuth()
  const { pathname } = useLocation()
  const isActive = (to: string) => pathname === to || pathname.startsWith(to + '/')

  return (
    <div className="min-h-screen bg-slate-50 text-slate-800">
      <header className="sticky top-0 z-10 border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-14 max-w-7xl items-center gap-6 px-4">
          <Link to="/" className="text-base font-bold text-indigo-700">
            F-Panda
          </Link>
          <nav className="flex gap-1" aria-label="メインメニュー">
            {mainNav.map((n) => (
              <NavLink key={n.to} item={n} active={isActive(n.to)} />
            ))}
            <NavLink item={{ to: '/masters/organizations', label: 'マスタ' }} active={pathname.startsWith('/masters')} />
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <span className="text-slate-600">
              {user.name}
              <span className="ml-1.5 rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-500">{roleLabels[user.role]}</span>
            </span>
            <button type="button" onClick={() => logout()} className="rounded px-2 py-1 text-slate-500 hover:bg-slate-100 hover:text-slate-700">
              ログアウト
            </button>
          </div>
        </div>
        {pathname.startsWith('/masters') && (
          <div className="border-t border-slate-100 bg-slate-50">
            <nav className="mx-auto flex max-w-7xl gap-1 px-4 py-1.5" aria-label="マスタメニュー">
              {masterNav.map((n) => (
                <NavLink key={n.to} item={n} active={isActive(n.to)} small />
              ))}
            </nav>
          </div>
        )}
      </header>
      <main className="mx-auto max-w-7xl px-4 py-6">{children}</main>
    </div>
  )
}

function NavLink({ item, active, small }: { item: NavItem; active: boolean; small?: boolean }) {
  return (
    <Link
      to={item.to}
      aria-current={active ? 'page' : undefined}
      className={cx(
        'rounded-md font-medium',
        small ? 'px-2.5 py-1 text-xs' : 'px-3 py-1.5 text-sm',
        active ? 'bg-indigo-50 text-indigo-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900',
      )}
    >
      {item.label}
    </Link>
  )
}
