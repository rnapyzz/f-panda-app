import { useState } from 'react'
import { api } from '../api/client'
import { cx } from './ui'

/** 重点施策の印（共有） */
export function PriorityBadge() {
  return (
    <span className="inline-flex items-center rounded bg-rose-50 px-1.5 py-0.5 text-xs font-medium whitespace-nowrap text-rose-700" title="重点施策">
      重点
    </span>
  )
}

/**
 * ウォッチ（気になる施策の印、本人にだけ見える）の切り替えボタン。
 * 押すとすぐ保存する。保存に失敗したら元に戻す。
 */
export function WatchButton({ activityId, name, watched, onChange, className }: { activityId: number; name: string; watched: boolean; onChange?: (watched: boolean) => void; className?: string }) {
  const [on, setOn] = useState(watched)
  const [busy, setBusy] = useState(false)
  // サーバーの値が変わったら合わせる
  const [synced, setSynced] = useState(watched)
  if (synced !== watched) {
    setSynced(watched)
    setOn(watched)
  }
  const toggle = async () => {
    const next = !on
    setOn(next)
    setBusy(true)
    try {
      if (next) await api.put(`/activities/${activityId}/watch`, {})
      else await api.del(`/activities/${activityId}/watch`)
      onChange?.(next)
    } catch {
      setOn(!next)
    } finally {
      setBusy(false)
    }
  }
  return (
    <button
      type="button"
      onClick={toggle}
      disabled={busy}
      aria-pressed={on}
      aria-label={on ? `${name}のウォッチを外す` : `${name}をウォッチする`}
      title={on ? 'ウォッチ中（自分にだけ見えます）' : 'ウォッチする（気になる施策の印。自分にだけ見えます）'}
      className={cx('rounded px-1 text-base leading-none', on ? 'text-amber-500 hover:text-amber-600' : 'text-slate-300 hover:text-slate-500', className)}
    >
      {on ? '★' : '☆'}
    </button>
  )
}
