// 図の共通の部品（施策の一覧の図・ダッシュボード）。カード・凡例・切り替え・ツールチップ・キーボード操作・文字の幅。

import { useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { Card, cx } from './ui'
import { formatAmount, formatSignedAmount } from '../lib/format'
import { useAmountUnit } from '../lib/amountUnit'
import { navigate } from '../lib/router'

/**
 * 図の金額の書式（画面で選んだ単位。docs/plan.md「7. 前提・決定事項」）。
 * man は目盛り・ラベル用（単位なし。単位は図の説明に出す）、yen・signedYen はツールチップ用（単位付き）。
 */
export function useChartFormat() {
  const { unit, label } = useAmountUnit()
  return {
    label,
    // 目盛り・ラベル: 整数のときは「.0」を省く（例: 百万円の目盛り「20」）
    man: (n: number, signed = false) => (signed ? formatSignedAmount(n, unit) : formatAmount(n, unit)).replace(/\.0$/, ''),
    yen: (n: number) => `${formatAmount(n, unit)} ${label}`,
    signedYen: (n: number) => `${formatSignedAmount(n, unit)} ${label}`,
  }
}

export function ChartCard({ legend, actions, footer, amounts = true, children }: { legend?: ReactNode; actions?: ReactNode; footer?: ReactNode; amounts?: boolean; children: ReactNode }) {
  const { label } = useAmountUnit()
  return (
    <Card>
      {(legend || actions) && (
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-slate-600" aria-label="凡例">
            {legend}
          </div>
          {actions && <div data-no-capture>{actions}</div>}
        </div>
      )}
      {children}
      {(footer || amounts) && (
        <p className="mt-3 text-xs text-slate-500">
          {footer}
          {amounts && <span className="ml-1">（金額の単位: {label}）</span>}
        </p>
      )}
    </Card>
  )
}

export function Swatch({ color, label, shape = 'dot' }: { color: string; label: ReactNode; shape?: 'dot' | 'square' | 'line' }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className={cx('inline-block shrink-0', shape === 'dot' ? 'size-2.5 rounded-full' : shape === 'square' ? 'size-3 rounded-[3px]' : 'h-1 w-4 rounded-full')}
        style={{ background: color }}
      />
      {label}
    </span>
  )
}

export function Segmented<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <div role="group" aria-label={label} className="inline-flex rounded-full bg-slate-100 p-0.5">
      {options.map(([v, l]) => (
        <button
          key={v}
          type="button"
          aria-pressed={value === v}
          onClick={() => onChange(v)}
          className={cx('rounded-full px-3 py-1 text-xs font-medium transition-colors', value === v ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-800')}
        >
          {l}
        </button>
      ))}
    </div>
  )
}

export type Tip = { x: number; y: number; content: ReactNode; flip: boolean }

/** 図の上に重ねるツールチップ。位置は図の枠からの px */
export function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null
  return (
    <div
      role="tooltip"
      data-no-capture
      className="pointer-events-none absolute z-20 w-64 rounded-lg bg-slate-900/95 px-3 py-2.5 text-xs text-slate-100 shadow-xl ring-1 ring-black/5"
      // 右端に近いときは、カーソルの左に出して枠からはみ出さないようにする（幅は w-64 = 256px）
      style={{ left: tip.flip ? Math.max(4, tip.x - 14 - 256) : tip.x + 14, top: tip.y + 14 }}
    >
      {tip.content}
    </div>
  )
}

/**
 * マウス・キーボードの両方で、図の印にツールチップを出し、クリック・Enter で動かす。
 * target が数値なら施策の画面を開き、関数ならそれを呼ぶ。null なら何もしない（ツールチップだけ）
 */
export function useMarks() {
  const [tip, setTip] = useState<Tip | null>(null)
  const [active, setActive] = useState<string | null>(null)
  const at = (e: MouseEvent | FocusEvent, key: string, content: ReactNode) => {
    const box = (e.currentTarget as Element).closest('[data-chart]')!.getBoundingClientRect()
    const r = (e.currentTarget as Element).getBoundingClientRect()
    const p = 'clientX' in e ? { x: e.clientX, y: e.clientY } : { x: r.left + r.width / 2, y: r.top + r.height / 2 }
    setActive(key)
    setTip({ x: p.x - box.left, y: p.y - box.top, content, flip: p.x - box.left + 14 + 256 > box.width })
  }
  const props = (key: string | number, target: number | (() => void) | null, label: string, content: ReactNode) => {
    const k = String(key)
    const open = () => {
      if (typeof target === 'number') navigate(`/activities/${target}`)
      else if (target) target()
    }
    return {
      tabIndex: 0,
      role: typeof target === 'number' ? 'link' : target ? 'button' : 'img',
      'aria-label': label,
      onMouseEnter: (e: MouseEvent) => at(e, k, content),
      onMouseMove: (e: MouseEvent) => at(e, k, content),
      onFocus: (e: FocusEvent) => at(e, k, content),
      onMouseLeave: () => (setTip(null), setActive(null)),
      onBlur: () => (setTip(null), setActive(null)),
      onClick: open,
      onKeyDown: (e: KeyboardEvent) => e.key === 'Enter' && open(),
      style: { cursor: target !== null ? 'pointer' : 'default', outline: 'none' },
    }
  }
  return { tip, active: (key: string | number) => active === String(key), anyActive: active !== null, props }
}

export function TipBody({ title, sub, rows, link = true, hint }: { title: string; sub?: string; rows: [string, string][]; link?: boolean; hint?: string }) {
  return (
    <>
      <div className="font-semibold text-white">{title}</div>
      {sub && <div className="mb-1.5 text-slate-400">{sub}</div>}
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
        {rows.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-slate-400">{k}</dt>
            <dd className="text-right tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      {(hint || link) && <div className="mt-1.5 text-[11px] text-slate-500">{hint ?? 'クリックで施策を開く'}</div>}
    </>
  )
}

/** 文字の幅の概算（全角は fontSize、半角は fontSize の 0.6 倍） */
export function textWidth(text: string, fontSize: number): number {
  let w = 0
  for (const ch of text) w += ch.charCodeAt(0) > 0xff ? fontSize : fontSize * 0.6
  return w
}

/** 幅に収まらない文字は省略する */
export function clip(text: string, width: number, fontSize: number): string {
  if (textWidth(text, fontSize) <= width) return text
  let out = ''
  for (const ch of text) {
    if (textWidth(`${out}${ch}…`, fontSize) > width) break
    out += ch
  }
  return out ? `${out}…` : ''
}

/**
 * 図を描く枠の幅（px）。SVG を実際の幅で描き、文字が縮まないようにする（狭い画面・2列の配置でも読める大きさ）
 */
export function useWidth<T extends HTMLElement>(initial = 640) {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(initial)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(Math.max(280, Math.round(el.getBoundingClientRect().width)))
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(280, Math.round(e.contentRect.width))))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, width] as const
}
