// History API を使った最小限のルーター。
// ルート定義は "/activities/:id" の形で書き、一致したパラメーターを返す。

import { useEffect, useSyncExternalStore, type AnchorHTMLAttributes, type MouseEvent } from 'react'

export { matchPath } from './path'

const listeners = new Set<() => void>()

function subscribe(fn: () => void) {
  listeners.add(fn)
  window.addEventListener('popstate', fn)
  return () => {
    listeners.delete(fn)
    window.removeEventListener('popstate', fn)
  }
}

function snapshot() {
  return window.location.pathname + window.location.search
}

/** 現在のパス（クエリを含む） */
export function useLocation(): { pathname: string; search: URLSearchParams } {
  const loc = useSyncExternalStore(subscribe, snapshot)
  const [pathname, search = ''] = loc.split('?')
  return { pathname, search: new URLSearchParams(search) }
}

export function navigate(to: string, opts?: { replace?: boolean }) {
  if (to === snapshot()) return
  if (opts?.replace) {
    window.history.replaceState(null, '', to)
  } else {
    window.history.pushState(null, '', to)
  }
  listeners.forEach((fn) => fn())
  window.scrollTo(0, 0)
}

type LinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & { to: string }

/** 画面内遷移のリンク。Ctrl/Cmd クリックなどはブラウザの標準動作に任せる */
export function Link({ to, onClick, ...rest }: LinkProps) {
  const handle = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e)
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    navigate(to)
  }
  return <a href={to} onClick={handle} {...rest} />
}

/** 表示時にリダイレクトする */
export function Redirect({ to }: { to: string }) {
  useEffect(() => navigate(to, { replace: true }), [to])
  return null
}
