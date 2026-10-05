// ホームの一覧の計算（利益・差・並べ替え）。金額は BigInt で扱う。

import type { ActivityProgress, NoteStatus, PLTotals } from '../api/types.ts'

export function profitOf(t: PLTotals | null | undefined): bigint | null {
  return t ? BigInt(t.revenue) - BigInt(t.expense) : null
}

/** 今回の利益 − 比較の利益。比較がなければ null */
export function profitDiff(current: PLTotals, compare: PLTotals | null): bigint | null {
  const c = profitOf(compare)
  return c === null ? null : profitOf(current)! - c
}

export type SortMode = 'status' | 'base' | 'previous'

const statusOrder: Record<NoteStatus, number> = { not_started: 0, in_progress: 1, completed: 2 }
const abs = (x: bigint | null) => (x === null ? -1n : x < 0n ? -x : x)

/**
 * 並べ替え。status は未完了（未着手 → 入力中）を先にして、同じ状態の中は基準との差の大きい順。
 * base・previous は、その比較との差（絶対値）の大きい順。
 */
export function sortStatuses(items: ActivityProgress[], mode: SortMode): ActivityProgress[] {
  const key = (a: ActivityProgress) => abs(profitDiff(a.current, mode === 'previous' ? a.previous : a.base))
  return [...items].sort((a, b) => {
    if (mode === 'status' && statusOrder[a.status] !== statusOrder[b.status]) return statusOrder[a.status] - statusOrder[b.status]
    const d = key(b) - key(a)
    if (d !== 0n) return d > 0n ? 1 : -1
    return a.code.localeCompare(b.code)
  })
}

/** 状態ごとの件数 */
export function countByStatus(items: ActivityProgress[]): Record<NoteStatus, number> {
  const out: Record<NoteStatus, number> = { not_started: 0, in_progress: 0, completed: 0 }
  for (const it of items) out[it.status]++
  return out
}

/** ユニットごとの合計（売上・利益）。比較のシナリオがなければ null */
export type UnitTotals = {
  unitId: number
  count: number
  open: number // 未完了の施策の数
  current: { revenue: bigint; profit: bigint }
  initial: { revenue: bigint; profit: bigint } | null
  revised: { revenue: bigint; profit: bigint } | null
  previous: { revenue: bigint; profit: bigint } | null
}

type Plans = 'current' | 'initial' | 'revised' | 'previous'

/** 施策の行をユニットごとに合計する（docs/plan.md「2.11」のサービスの状況）。has は、その比較のシナリオがあるか */
export function totalsByUnit(items: ActivityProgress[], has: Record<Exclude<Plans, 'current'>, boolean>): UnitTotals[] {
  const out = new Map<number, UnitTotals>()
  const zero = () => ({ revenue: 0n, profit: 0n })
  for (const it of items) {
    let t = out.get(it.unit_id)
    if (!t) {
      t = { unitId: it.unit_id, count: 0, open: 0, current: zero(), initial: has.initial ? zero() : null, revised: has.revised ? zero() : null, previous: has.previous ? zero() : null }
      out.set(it.unit_id, t)
    }
    t.count++
    if (it.status !== 'completed') t.open++
    for (const plan of ['current', 'initial', 'revised', 'previous'] as Plans[]) {
      const target = t[plan]
      const src = it[plan]
      if (!target || !src) continue
      target.revenue += BigInt(src.revenue)
      target.profit += BigInt(src.revenue) - BigInt(src.expense)
    }
  }
  return [...out.values()]
}

/** 合計の行 */
export function sumTotals(rows: UnitTotals[], has: Record<Exclude<Plans, 'current'>, boolean>): UnitTotals {
  const t: UnitTotals = { unitId: 0, count: 0, open: 0, current: { revenue: 0n, profit: 0n }, initial: has.initial ? { revenue: 0n, profit: 0n } : null, revised: has.revised ? { revenue: 0n, profit: 0n } : null, previous: has.previous ? { revenue: 0n, profit: 0n } : null }
  for (const r of rows) {
    t.count += r.count
    t.open += r.open
    for (const plan of ['current', 'initial', 'revised', 'previous'] as Plans[]) {
      const a = t[plan]
      const b = r[plan]
      if (!a || !b) continue
      a.revenue += b.revenue
      a.profit += b.profit
    }
  }
  return t
}
