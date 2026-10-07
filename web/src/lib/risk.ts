// リスク画面の集計（docs/plan.md「2.8」「2.9」）。金額は BigInt で扱う。

import type { PL, RiskActivity, RiskReport } from '../api/types.ts'

export type Measure = 'full' | 'weighted' | 'optimistic' | 'pessimistic' | 'actual'

export type Totals = Record<Measure, { revenue: bigint; expense: bigint }>

export const profitOf = (p: { revenue: bigint; expense: bigint }): bigint => p.revenue - p.expense

const toBig = (p: PL) => ({ revenue: BigInt(p.revenue), expense: BigInt(p.expense) })

/** 施策の指標を合計する */
export function sumMeasures(items: RiskActivity[]): Totals {
  const zero = () => ({ revenue: 0n, expense: 0n })
  const out: Totals = { full: zero(), weighted: zero(), optimistic: zero(), pessimistic: zero(), actual: zero() }
  for (const a of items) {
    for (const k of Object.keys(out) as Measure[]) {
      const v = toBig(a[k])
      out[k].revenue += v.revenue
      out[k].expense += v.expense
    }
  }
  return out
}

/** 比較シナリオの加重見込の利益の合計（比較がなければ null） */
export function compareProfit(items: RiskActivity[]): bigint | null {
  let any = false
  let total = 0n
  for (const a of items) {
    if (!a.compare) continue
    any = true
    total += profitOf(toBig(a.compare))
  }
  return any ? total : null
}

/** 構成のキー（実績・段階・ダウンサイド）の順。段階はマスタの表示順 */
export function compositionKeys(levels: RiskReport['levels']): string[] {
  return ['actual', ...levels.map((l) => l.code), 'downside']
}

/** 売上の構成（キー → 金額）を合計する */
export function sumComposition(items: RiskActivity[], keys: string[]): Map<string, bigint> {
  const out = new Map(keys.map((k) => [k, 0n]))
  for (const a of items) {
    for (const [k, v] of Object.entries(a.revenue_by_level)) {
      out.set(k, (out.get(k) ?? 0n) + BigInt(v))
    }
  }
  return out
}

/**
 * 確度の高い見込の割合（%）: （実績 ＋ 確度の高い段階の売上）÷ 満額の売上。満額が 0 以下なら null。
 * ダウンサイドは満額に含めない（マイナスで割合がゆがむため）。
 */
export function highCertaintyRate(composition: Map<string, bigint>, levels: RiskReport['levels']): number | null {
  const high = new Set(levels.filter((l) => l.high).map((l) => l.code))
  let sure = 0n
  let total = 0n
  for (const [k, v] of composition) {
    if (k === 'downside') continue
    total += v
    if (k === 'actual' || high.has(k)) sure += v
  }
  if (total <= 0n) return null
  return Number((sure * 1000n) / total) / 10
}

export type SortKey = 'spread' | 'warnings' | 'compare' | 'code'

/** 振れ幅（楽観 − 悲観、利益） */
export const spreadOf = (a: RiskActivity) => profitOf(toBig(a.optimistic)) - profitOf(toBig(a.pessimistic))

/** 比較シナリオとの差（加重見込の利益）。比較がなければ null */
export const compareDiffOf = (a: RiskActivity) => (a.compare ? profitOf(toBig(a.weighted)) - profitOf(toBig(a.compare)) : null)

const abs = (x: bigint) => (x < 0n ? -x : x)

/** 施策の並べ替え。振れ幅・差は大きい順、警告は「段階に対して状況が悪い」→ 警告の数の順 */
export function sortActivities(items: RiskActivity[], key: SortKey): RiskActivity[] {
  const out = [...items]
  const cmpBig = (x: bigint, y: bigint) => (x === y ? 0 : x > y ? -1 : 1)
  out.sort((a, b) => {
    let c = 0
    if (key === 'spread') c = cmpBig(spreadOf(a), spreadOf(b))
    if (key === 'warnings') c = Number(b.bad_for_level) - Number(a.bad_for_level) || b.warning_count - a.warning_count
    if (key === 'compare') c = cmpBig(abs(compareDiffOf(a) ?? 0n), abs(compareDiffOf(b) ?? 0n))
    return c || a.code.localeCompare(b.code)
  })
  return out
}

/** 金額がある（満額・実績のどれかが 0 でない）施策か */
export const hasAmounts = (a: RiskActivity) =>
  [a.full.revenue, a.full.expense, a.optimistic.revenue, a.pessimistic.revenue].some((v) => v !== '0') || Object.keys(a.revenue_by_level).length > 0

export type WarningKind = 'milestone' | 'postponed' | 'downward' | 'consecutive' | 'accuracy'

export const warningLabels: Record<WarningKind, string> = {
  milestone: 'マイルストーンの遅れ',
  postponed: 'マイルストーンの後ろ倒し',
  downward: '下方修正',
  consecutive: '連続の下方修正',
  accuracy: '見込の当たり具合',
}

/** 施策の警告の種類 */
export function warningKinds(a: RiskActivity): WarningKind[] {
  const out: WarningKind[] = []
  if (a.warnings.milestones.some((m) => m.risk !== 'upcoming')) out.push('milestone')
  if (a.warnings.postponed.count > 0) out.push('postponed')
  if (a.warnings.downward) out.push('downward')
  if (a.warnings.consecutive) out.push('consecutive')
  if (a.warnings.accuracy !== null) out.push('accuracy')
  return out
}
