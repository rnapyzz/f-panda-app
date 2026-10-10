import { createContext, useContext, type ReactNode } from 'react'
import { amountUnitLabels, formatAmount, formatSignedAmount, type AmountUnit } from './format'

// 確認・報告の画面の金額の表示単位（docs/plan.md「7. 前提・決定事項」）。画面ごとに AmountUnitProvider で単位を渡し、
// 金額を表示する部品は useAmountUnit で書式を得る。Provider の外は百万円。

const AmountUnitContext = createContext<AmountUnit>('million')

export function AmountUnitProvider({ unit, children }: { unit: AmountUnit; children: ReactNode }) {
  return <AmountUnitContext.Provider value={unit}>{children}</AmountUnitContext.Provider>
}

/** 表示単位と書式。fmt は金額、signed は差（+ / ▲）、label は「百万円」などの単位名 */
export function useAmountUnit() {
  const unit = useContext(AmountUnitContext)
  return {
    unit,
    label: amountUnitLabels[unit],
    fmt: (v: bigint | string | number | null | undefined) => formatAmount(v, unit),
    signed: (v: bigint | string | number | null | undefined) => formatSignedAmount(v, unit),
  }
}
