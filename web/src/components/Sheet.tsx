import { useEffect, useRef, useState, type ClipboardEvent, type FocusEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { cleanNumber, fillCells, inRange, move, parseClipboard, pasteCells, rangeOf, toTsv, type Pos } from '../lib/sheet'
import { cx } from './ui'

/**
 * スプレッドシート風の入力グリッド。
 *
 * - クリック・Shift+クリック・ドラッグ・Shift+矢印で範囲を選ぶ。Ctrl/⌘+A で全体
 * - そのまま入力すると上書き、Enter・F2・ダブルクリックで編集。Enter で下へ、Tab で右へ移動し、Esc で取り消す
 * - Delete・Backspace で範囲を消去。Ctrl/⌘+C・X・V でコピー・切り取り・貼り付け（Excel・Google スプレッドシートと相互に使える）
 * - Ctrl/⌘+D で下方向、Ctrl/⌘+R で右方向にコピー。Ctrl/⌘+Z で元に戻す、Ctrl/⌘+Shift+Z・Ctrl/⌘+Y でやり直す
 *
 * grid はセルのキーの行列（null はセルなし）。値の読み書きは呼び出し側が持つ。
 */
export type SheetOptions<K extends string> = {
  grid: (K | null)[][]
  isEditable: (k: K) => boolean
  rawValue: (k: K) => string
  setValues: (entries: [K, string][]) => void
  onActivate?: (k: K) => void
  onUndo?: () => void
  onRedo?: () => void
}

type Editing = { value: string; /** F2・Enter で始めた編集。矢印キーはカーソル移動に使う */ caret: boolean }

export type Sheet = ReturnType<typeof useSheet>

export function useSheet<K extends string>(o: SheetOptions<K>) {
  const ref = useRef<HTMLDivElement>(null)
  const [anchor, setAnchor] = useState<Pos | null>(null)
  const [focus, setFocus] = useState<Pos | null>(null)
  const [editing, setEditingState] = useState<Editing | null>(null)
  const editingRef = useRef<Editing | null>(null)
  const [hasFocus, setHasFocus] = useState(false)
  const dragging = useRef(false)

  const rows = o.grid.length
  const cols = o.grid[0]?.length ?? 0
  const keyAt = (p: Pos) => o.grid[p.r]?.[p.c] ?? null
  const range = anchor && focus ? rangeOf(anchor, focus) : null

  const setEditing = (e: Editing | null) => {
    editingRef.current = e
    setEditingState(e)
  }

  useEffect(() => {
    const up = () => (dragging.current = false)
    window.addEventListener('mouseup', up)
    return () => window.removeEventListener('mouseup', up)
  }, [])

  // 選択中のセルを見える位置までスクロールする
  useEffect(() => {
    ref.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [focus])

  const select = (p: Pos, extend = false) => {
    if (!extend || !anchor) setAnchor(p)
    setFocus(p)
    const k = keyAt(p)
    if (k && !extend) o.onActivate?.(k)
  }

  const apply = (cells: { r: number; c: number; value: string }[]) => {
    const entries: [K, string][] = []
    for (const { r, c, value } of cells) {
      const k = o.grid[r]?.[c]
      if (k && o.isEditable(k)) entries.push([k, cleanNumber(value)])
    }
    if (entries.length > 0) o.setValues(entries)
  }

  const startEdit = (initial?: string) => {
    const k = focus && keyAt(focus)
    if (!k || !o.isEditable(k)) return
    setEditing({ value: initial ?? o.rawValue(k), caret: initial === undefined })
  }

  const commit = () => {
    const e = editingRef.current
    const k = focus && keyAt(focus)
    setEditing(null)
    if (e && k && cleanNumber(e.value) !== o.rawValue(k)) o.setValues([[k, cleanNumber(e.value)]])
  }

  const refocus = () => ref.current?.focus({ preventScroll: true })

  const matrixOf = () => {
    if (!range) return []
    const m: string[][] = []
    for (let r = range.top; r <= range.bottom; r++) {
      const row: string[] = []
      for (let c = range.left; c <= range.right; c++) {
        const k = o.grid[r]?.[c]
        row.push(k ? o.rawValue(k) : '')
      }
      m.push(row)
    }
    return m
  }

  const onKeyDown = (e: KeyboardEvent) => {
    if (editingRef.current || !focus || !range) return
    const mod = e.metaKey || e.ctrlKey
    const arrows: Record<string, [number, number]> = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] }
    if (arrows[e.key]) {
      e.preventDefault()
      const [dr, dc] = arrows[e.key]
      const next = move(focus, dr, dc, rows, cols, mod)
      if (e.shiftKey) setFocus(next)
      else select(next)
      return
    }
    if (e.key === 'Tab') {
      const next = move(focus, 0, e.shiftKey ? -1 : 1, rows, cols)
      if (next.c === focus.c) return // 端ではグリッドの外にフォーカスを移す
      e.preventDefault()
      select(next)
      return
    }
    if (e.key === 'Enter' || e.key === 'F2') {
      e.preventDefault()
      startEdit()
      return
    }
    if (e.key === 'Delete' || e.key === 'Backspace') {
      e.preventDefault()
      apply(matrixOf().flatMap((row, i) => row.map((_, j) => ({ r: range.top + i, c: range.left + j, value: '' }))))
      return
    }
    if (e.key === 'Escape') {
      setAnchor(focus)
      return
    }
    if (mod && !e.altKey) {
      const key = e.key.toLowerCase()
      if (key === 'a') {
        e.preventDefault()
        setAnchor({ r: 0, c: 0 })
        setFocus({ r: rows - 1, c: cols - 1 })
      } else if (key === 'd' || key === 'r') {
        e.preventDefault()
        apply(fillCells(range, key === 'd' ? 'down' : 'right', (r, c) => {
          const k = o.grid[r]?.[c]
          return k ? o.rawValue(k) : ''
        }))
      } else if ((key === 'z' && e.shiftKey) || key === 'y') {
        e.preventDefault()
        o.onRedo?.()
      } else if (key === 'z') {
        e.preventDefault()
        o.onUndo?.()
      }
      return
    }
    if (e.key.length === 1 && !e.altKey) {
      e.preventDefault()
      startEdit(e.key)
    }
  }

  const onEditorKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || !focus) return
    e.stopPropagation()
    const go = (dr: number, dc: number) => {
      e.preventDefault()
      commit()
      select(move(focus, dr, dc, rows, cols))
      refocus()
    }
    if (e.key === 'Enter') go(e.shiftKey ? -1 : 1, 0)
    else if (e.key === 'Tab') go(0, e.shiftKey ? -1 : 1)
    else if (e.key === 'Escape') {
      e.preventDefault()
      setEditing(null)
      refocus()
    } else if (!editingRef.current?.caret) {
      const arrows: Record<string, [number, number]> = { ArrowUp: [-1, 0], ArrowDown: [1, 0], ArrowLeft: [0, -1], ArrowRight: [0, 1] }
      if (arrows[e.key]) go(...arrows[e.key])
    }
  }

  return {
    containerProps: {
      ref,
      tabIndex: 0,
      onKeyDown,
      onFocus: () => setHasFocus(true),
      onBlur: (e: FocusEvent) => {
        if (ref.current?.contains(e.relatedTarget as Node | null)) return
        setHasFocus(false)
        if (editingRef.current) commit()
      },
      onCopy: (e: ClipboardEvent) => {
        if (editingRef.current || !range) return
        e.preventDefault()
        e.clipboardData.setData('text/plain', toTsv(matrixOf()))
      },
      onCut: (e: ClipboardEvent) => {
        if (editingRef.current || !range) return
        e.preventDefault()
        const m = matrixOf()
        e.clipboardData.setData('text/plain', toTsv(m))
        apply(m.flatMap((row, i) => row.map((_, j) => ({ r: range.top + i, c: range.left + j, value: '' }))))
      },
      onPaste: (e: ClipboardEvent) => {
        if (editingRef.current || !range) return
        e.preventDefault()
        const cells = pasteCells(range, parseClipboard(e.clipboardData.getData('text/plain')), rows, cols)
        if (cells.length === 0) return
        apply(cells)
        // 貼り付けた範囲を選択する
        setAnchor({ r: cells[0].r, c: cells[0].c })
        setFocus({ r: cells[cells.length - 1].r, c: cells[cells.length - 1].c })
      },
    },
    /** セルの表示状態とマウス操作 */
    cell: (r: number, c: number) => {
      const active = focus?.r === r && focus?.c === c
      return {
        active,
        focused: hasFocus,
        inSelection: range !== null && inRange(range, r, c) && !(range.top === range.bottom && range.left === range.right),
        editing: active ? editing : null,
        onMouseDown: (e: MouseEvent) => {
          if (active && editingRef.current) return
          e.preventDefault()
          if (editingRef.current) commit()
          refocus()
          select({ r, c }, e.shiftKey)
          dragging.current = true
        },
        onMouseEnter: () => {
          if (dragging.current) setFocus({ r, c })
        },
        onDoubleClick: () => startEdit(),
        onEditorChange: (value: string) => setEditing({ value, caret: editingRef.current?.caret ?? false }),
        onEditorKeyDown,
        onEditorBlur: () => {
          if (editingRef.current) commit()
        },
      }
    },
  }
}

/** グリッドの外枠。キーボード操作・クリップボードはここで受ける */
export function SheetFrame({ sheet, label, children }: { sheet: Sheet; label: string; children: ReactNode }) {
  return (
    <div {...sheet.containerProps} className="-mx-4 scroll-pl-48 overflow-x-auto outline-none">
      <table role="grid" aria-label={label} aria-multiselectable className="min-w-full border-separate border-spacing-0 text-sm select-none">
        {children}
      </table>
    </div>
  )
}

/** グリッドの1セル */
export function SheetCell({
  sheet,
  r,
  c,
  label,
  display,
  editable,
  provisional,
  changed,
  error,
}: {
  sheet: Sheet
  r: number
  c: number
  label: string
  display: string
  editable: boolean
  provisional: boolean
  changed: boolean
  error?: string
}) {
  const s = sheet.cell(r, c)
  return (
    <td
      role="gridcell"
      aria-label={`${label}: ${display || '未入力'}`}
      aria-selected={s.active || s.inSelection}
      aria-readonly={!editable}
      data-active={s.active}
      title={error ?? (provisional ? '仮の値' : !editable ? '計算式で算出（直接入力できません）' : undefined)}
      onMouseDown={s.onMouseDown}
      onMouseEnter={s.onMouseEnter}
      onDoubleClick={s.onDoubleClick}
      className="relative border-r border-b border-slate-100 p-0"
    >
      {s.editing ? (
        <input
          autoFocus
          aria-label={label}
          inputMode="decimal"
          value={s.editing.value}
          onChange={(e) => s.onEditorChange(e.target.value)}
          onKeyDown={s.onEditorKeyDown}
          onBlur={s.onEditorBlur}
          onFocus={(e) => {
            const len = e.target.value.length
            e.target.setSelectionRange(len, len)
          }}
          className="block h-8 w-28 bg-white px-2 text-right tabular-nums outline-2 -outline-offset-2 outline-indigo-600"
        />
      ) : (
        <div
          className={cx(
            'flex h-8 w-28 items-center justify-end px-2 tabular-nums',
            editable ? 'cursor-cell text-slate-800' : 'cursor-default bg-slate-50/70 text-slate-500',
            provisional && 'bg-amber-50',
            changed && 'font-medium text-indigo-700',
            s.inSelection && 'bg-indigo-100/70',
            changed && !s.active && 'shadow-[inset_0_-2px_0_0_var(--color-indigo-300)]',
            error && 'outline-2 -outline-offset-2 outline-red-400',
            s.active && (s.focused ? 'outline-2 -outline-offset-2 outline-indigo-600' : 'outline-2 -outline-offset-2 outline-slate-300'),
          )}
        >
          {display || (editable ? '' : <span className="text-slate-300">—</span>)}
        </div>
      )}
    </td>
  )
}
