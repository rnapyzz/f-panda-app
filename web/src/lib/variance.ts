// 今回の見込と比較シナリオの差の要約（差異の説明を書く材料）。金額は BigInt で扱う。

import type { AmountRow } from '../api/types.ts'
import { buildSeriesPl, sumOver } from './pl.ts'

export type VarianceItem = { label: string; diff: bigint }

export type VarianceSummary = {
  /** 年間の利益: 今回、比較、差 */
  current: bigint
  compare: bigint
  diff: bigint
  /** 差（絶対値）の大きい科目（収益・費用）。差が 0 のものは含めない */
  subjects: VarianceItem[]
  /** 利益の差（絶対値）の大きい月 */
  months: (VarianceItem & { month: string })[]
}

/** 今回と比較シナリオの、年間の利益の差と、差の大きい科目・月（上位 limit 件） */
export function summarizeVariance(current: AmountRow[], compare: AmountRow[], months: string[], limit = 3): VarianceSummary {
  const [revenue, expense, profit] = buildSeriesPl([current, compare])
  const abs = (x: bigint) => (x < 0n ? -x : x)
  const byAbs = (a: VarianceItem, b: VarianceItem) => (abs(b.diff) > abs(a.diff) ? 1 : abs(b.diff) < abs(a.diff) ? -1 : 0)
  const subjects = [...revenue.children, ...expense.children]
    .map((n) => ({ label: n.label, diff: sumOver(n.values[0], months) - sumOver(n.values[1], months) }))
    .filter((x) => x.diff !== 0n)
    .sort(byAbs)
    .slice(0, limit)
  const monthItems = months
    .map((m) => ({ month: m, label: `${Number(m.slice(5, 7))}月`, diff: sumOver(profit.values[0], [m]) - sumOver(profit.values[1], [m]) }))
    .filter((x) => x.diff !== 0n)
    .sort(byAbs)
    .slice(0, limit)
  const cur = sumOver(profit.values[0], months)
  const cmp = sumOver(profit.values[1], months)
  return { current: cur, compare: cmp, diff: cur - cmp, subjects, months: monthItems }
}

/**
 * 差が大きいか（docs/plan.md「2.10」: 年間の利益の差が、比較の 10% 以上かつ 10万円以上）。
 * 説明がないまま完了にするときの確認に使う。
 */
export function isLargeVariance(diff: bigint, compare: bigint): boolean {
  const abs = (x: bigint) => (x < 0n ? -x : x)
  return abs(diff) >= 100000n && abs(diff) * 10n >= abs(compare)
}
