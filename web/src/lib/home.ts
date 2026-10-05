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
