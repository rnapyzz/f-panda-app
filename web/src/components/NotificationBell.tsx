import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import type { AppNotification, NotificationList } from '../api/types'
import { formatDateTime } from '../lib/format'
import { navigate } from '../lib/router'
import { useApi } from '../lib/useApi'
import { IconBell } from './icons'
import { cx } from './ui'

/** お知らせを読み直す間隔 */
const POLL_MS = 60_000

/**
 * アプリ内のお知らせ（docs/plan.md「2.13」）。ベルのアイコンと未読の数を出し、開くと自分宛てのお知らせを表示する。
 * お知らせを開くと既読にして、リンク先（ホームなど）へ移動する。
 */
export function NotificationBell() {
  const list = useApi<NotificationList>('/notifications?limit=20')
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const { reload } = list

  useEffect(() => {
    const timer = window.setInterval(() => void reload().catch(() => undefined), POLL_MS)
    return () => window.clearInterval(timer)
  }, [reload])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  const unread = list.data?.unread ?? 0
  const items = list.data?.items ?? []

  const openItem = async (n: AppNotification) => {
    setOpen(false)
    if (!n.read_at) {
      await api.post(`/notifications/${n.id}/read`, {}).catch(() => undefined)
      await reload()
    }
    navigate(n.link || '/')
  }
  const readAll = async () => {
    await api.post('/notifications/read-all', {})
    await reload()
  }

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        aria-label={unread > 0 ? `お知らせ（未読 ${unread} 件）` : 'お知らせ'}
        className="relative rounded p-1 text-emerald-900 hover:bg-emerald-100"
      >
        <IconBell className="size-5" />
        {unread > 0 && (
          <span className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-red-600 px-1 text-center text-[10px] leading-4 font-semibold text-white tabular-nums">
            {unread > 99 ? '99+' : unread}
          </span>
        )}
      </button>
      {open && (
        <div role="dialog" aria-label="お知らせ" className="absolute top-8 right-0 z-30 w-[min(24rem,calc(100vw-2rem))] rounded-lg border border-slate-200 bg-white text-slate-800 shadow-xl">
          <div className="flex items-center justify-between border-b border-slate-100 px-3 py-2">
            <span className="text-sm font-semibold">お知らせ</span>
            {unread > 0 && (
              <button type="button" onClick={readAll} className="text-xs text-indigo-700 hover:underline">
                すべて既読にする
              </button>
            )}
          </div>
          {items.length === 0 ? (
            <p className="px-3 py-6 text-center text-sm text-slate-500">お知らせはありません</p>
          ) : (
            <ul className="max-h-[60vh] divide-y divide-slate-100 overflow-y-auto">
              {items.map((n) => (
                <li key={n.id}>
                  <button type="button" onClick={() => openItem(n)} className={cx('block w-full px-3 py-2 text-left hover:bg-slate-50', !n.read_at && 'bg-indigo-50/60')}>
                    <span className="flex items-start gap-2">
                      {!n.read_at && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-indigo-600" aria-label="未読" />}
                      <span className="min-w-0">
                        <span className="block text-sm font-medium">{n.title}</span>
                        <span className="mt-0.5 line-clamp-3 block text-xs whitespace-pre-line text-slate-600">{n.body}</span>
                        <span className="mt-0.5 block text-[11px] text-slate-400">{formatDateTime(n.created_at)}</span>
                      </span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
