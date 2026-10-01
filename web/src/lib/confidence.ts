// 確度の段階の表示。

import type { ConfidenceLevel } from '../api/types.ts'

/** 標準の確率（0〜1）をパーセントにする（例: 0.8 → "80%"） */
export function ratePercent(rate: number | string): string {
  return `${Math.round(Number(rate) * 1000) / 10}%`
}

/** 「C 中（50%）」。マスタにない段階はコードだけ */
export function confidenceLabel(code: string, levels: ConfidenceLevel[] | undefined): string {
  const l = levels?.find((x) => x.code === code)
  return l ? `${l.code} ${l.name}（${ratePercent(l.rate)}）` : code
}
