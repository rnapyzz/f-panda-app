// 予実比較の集計。金額は BigInt で扱い、誤差を出さない。

import type { ReportRow, SubjectCategory } from '../api/types'

export type Measure = 'profit' | 'revenue' | 'expense'

export const measureLabels: Record<Measure, string> = {
  profit: '利益',
  revenue: '収益',
  expense: '費用',
}

/** 系列ごとの集計結果 */
export type Totals = {
  revenue: bigint
  expense: bigint
  bySubject: Map<number, bigint>
  byMonth: Map<string, { revenue: bigint; expense: bigint }>
}

function emptyTotals(): Totals {
  return { revenue: 0n, expense: 0n, bySubject: new Map(), byMonth: new Map() }
}

/**
 * rows のうち include を満たす行を、系列ごとに集計する。
 * months に含まれる月だけを対象にする（期間の絞り込み）。
 */
export function aggregate(
  rows: ReportRow[],
  seriesKeys: string[],
  categoryOf: (subjectId: number) => SubjectCategory | undefined,
  include: (row: ReportRow) => boolean,
  months: Set<string>,
): Map<string, Totals> {
  const out = new Map(seriesKeys.map((k) => [k, emptyTotals()]))
  for (const row of rows) {
    if (!include(row)) continue
    const category = categoryOf(row.subject_id)
    if (!category) continue
    for (const key of seriesKeys) {
      const raw = row.values[key]
      if (raw === undefined) continue
      const v = BigInt(raw)
      const t = out.get(key)!
      // 月別は期間に関係なく全月を持つ（月次推移の表示用）
      const m = t.byMonth.get(row.month) ?? { revenue: 0n, expense: 0n }
      m[category] += v
      t.byMonth.set(row.month, m)
      if (!months.has(row.month)) continue
      t[category] += v
      t.bySubject.set(row.subject_id, (t.bySubject.get(row.subject_id) ?? 0n) + v)
    }
  }
  return out
}

export function measureOf(t: { revenue: bigint; expense: bigint } | undefined, measure: Measure): bigint {
  if (!t) return 0n
  if (measure === 'revenue') return t.revenue
  if (measure === 'expense') return t.expense
  return t.revenue - t.expense
}

/** 差異率（%）。基準が 0 のときは null */
export function varianceRate(base: bigint, value: bigint): number | null {
  if (base === 0n) return null
  const diff = value - base
  // BigInt のまま 0.1% 単位で計算してから数値にする
  return Number((diff * 1000n) / (base < 0n ? -base : base)) / 10
}

/**
 * 差異が良い方向か。収益・利益は増えると良い、費用は増えると悪い。
 */
export function isFavorable(diff: bigint, measure: Measure | SubjectCategory): boolean | null {
  if (diff === 0n) return null
  return measure === 'expense' ? diff < 0n : diff > 0n
}
