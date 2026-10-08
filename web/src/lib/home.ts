// ホームの一覧の計算（利益・差・並べ替え）。金額は BigInt で扱う。

import { noteCauseLabels, type ActivityProgress, type NoteCause, type NoteStatus, type PLTotals } from '../api/types.ts'

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

/** 変動のサマリーの1施策 */
export type ChangeItem = { item: ActivityProgress; diff: bigint }

export type ChangeSummary = {
  /** 前回見込があるか（なければ変動を出さない） */
  hasPrevious: boolean
  profitDiff: bigint
  revenueDiff: bigint
  /** 期初計画との利益の差の合計（期初計画がなければ null） */
  initialDiff: bigint | null
  increased: number
  decreased: number
  /** 利益の変動の大きい施策（上位） */
  top: ChangeItem[]
  /** 要因別の利益の変動。single は要因1つ、multiple は複数、none は要因なし（重ねて数えない） */
  byCause: { key: NoteCause | 'multiple' | 'none'; diff: bigint; count: number }[]
  /** 変動があるのに説明のない施策の数 */
  unexplained: number
  /** 未完了の施策の数 */
  open: number
}

/** 前回見込 → 今回の変動を、施策別・要因別にまとめる（docs/plan.md「2.11」の ②） */
export function summarizeChanges(items: ActivityProgress[], limit = 5): ChangeSummary {
  const changes: ChangeItem[] = []
  let profitTotal = 0n
  let revenueTotal = 0n
  let initialTotal: bigint | null = null
  const byCause = new Map<NoteCause | 'multiple' | 'none', { diff: bigint; count: number }>()
  let hasPrevious = false
  for (const it of items) {
    const d = profitDiff(it.current, it.initial)
    if (d !== null) initialTotal = (initialTotal ?? 0n) + d
    if (!it.previous) continue
    hasPrevious = true
    const diff = profitDiff(it.current, it.previous)!
    profitTotal += diff
    revenueTotal += BigInt(it.current.revenue) - BigInt(it.previous.revenue)
    if (diff === 0n) continue
    changes.push({ item: it, diff })
    const key = it.causes.length === 0 ? 'none' : it.causes.length === 1 ? it.causes[0] : 'multiple'
    const c = byCause.get(key) ?? { diff: 0n, count: 0 }
    c.diff += diff
    c.count++
    byCause.set(key, c)
  }
  const absDiff = (x: bigint) => (x < 0n ? -x : x)
  const top = [...changes].sort((a, b) => (absDiff(b.diff) > absDiff(a.diff) ? 1 : absDiff(b.diff) < absDiff(a.diff) ? -1 : a.item.code.localeCompare(b.item.code))).slice(0, limit)
  return {
    hasPrevious,
    profitDiff: profitTotal,
    revenueDiff: revenueTotal,
    initialDiff: initialTotal,
    increased: changes.filter((c) => c.diff > 0n).length,
    decreased: changes.filter((c) => c.diff < 0n).length,
    top,
    byCause: [...byCause.entries()].map(([key, v]) => ({ key, ...v })).sort((a, b) => (absDiff(b.diff) > absDiff(a.diff) ? 1 : absDiff(b.diff) < absDiff(a.diff) ? -1 : 0)),
    unexplained: changes.filter((c) => !c.item.has_explanation).length,
    open: items.filter((it) => it.status !== 'completed').length,
  }
}

/** 金額を「−1,200万円」のように短く表す（1万円未満は円のまま）。文章のサマリー用 */
export function compactYen(v: bigint): string {
  const sign = v < 0n ? '−' : v > 0n ? '+' : ''
  const abs = v < 0n ? -v : v
  if (abs < 10000n) return `${sign}${abs.toLocaleString('ja-JP')}円`
  const man = (abs + 5000n) / 10000n // 万円未満を四捨五入
  return `${sign}${man.toLocaleString('ja-JP')}万円`
}

/** 文章のサマリー（決まった型で組み立てる。docs/plan.md「2.11」の ②） */
export function summaryText(s: ChangeSummary): string {
  const parts = [`前回の見込から利益 ${compactYen(s.profitDiff)}（売上 ${compactYen(s.revenueDiff)}）。`, `増加 ${s.increased}施策・減少 ${s.decreased}施策。`]
  if (s.top.length > 0) {
    const main = s.top.slice(0, 3).map((c) => {
      const causes = c.item.causes.map((x) => noteCauseLabels[x]).join('・')
      return `${c.item.name} ${compactYen(c.diff)}${causes ? `（${causes}）` : ''}`
    })
    parts.push(`主な変動: ${main.join('、')}。`)
  }
  const notes = [s.unexplained > 0 && `説明のない施策 ${s.unexplained}件`, s.open > 0 && `未完了 ${s.open}件`].filter(Boolean)
  if (notes.length > 0) parts.push(`${notes.join('、')}。`)
  return parts.join('')
}
