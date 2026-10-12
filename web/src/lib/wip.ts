// 検収月に一括の施策の原価と仕掛（docs/plan.md「2.28」）。金額は BigInt で扱う。
//
// 原価は発生した月に入れ、仕掛品振替（計上月より前は発生原価のマイナス、計上月は仕掛残高の戻し）はサーバーが計算する。
// 画面では、発生原価・振替・売上原価（発生原価 + 振替）・月末仕掛残高（振替の累計の符号を反転したもの）を月ごとに出す。

import type { AmountRow, ValuesView } from '../api/types'

export type WipTable = {
  months: string[]
  /** 原価の科目ごとの発生原価 */
  costs: { subjectId: number; name: string; values: Map<string, bigint> }[]
  incurred: Map<string, bigint>
  transfer: Map<string, bigint>
  cogs: Map<string, bigint>
  balance: Map<string, bigint>
  /** 売上（収益の合計） */
  revenue: Map<string, bigint>
}

/** 科目の月ごとの金額（科目への直接入力と内訳の合計） */
function subjectTotals(row: AmountRow): Map<string, bigint> {
  const out = new Map<string, bigint>()
  for (const c of [...row.values, ...row.lines.flatMap((l) => l.values)]) out.set(c.target_month, (out.get(c.target_month) ?? 0n) + BigInt(c.amount))
  return out
}

/** 数値入力 API の値から、原価と仕掛の表を組み立てる。検収月に一括でない施策は null */
export function buildWip(view: Pick<ValuesView, 'months' | 'beyond_months' | 'amounts' | 'wip'>): WipTable | null {
  const wip = view.wip
  if (!wip) return null
  const months = [...view.months, ...view.beyond_months]
  const costIds = new Set(wip.cost_subject_ids)
  const costs = view.amounts.filter((r) => costIds.has(r.subject_id)).map((r) => ({ subjectId: r.subject_id, name: r.name, values: subjectTotals(r) }))
  const transferRow = view.amounts.find((r) => r.subject_id === wip.transfer_subject_id)
  const transfer = transferRow ? subjectTotals(transferRow) : new Map<string, bigint>()
  const revenue = new Map<string, bigint>()
  for (const r of view.amounts) {
    if (r.category !== 'revenue') continue
    for (const [m, v] of subjectTotals(r)) revenue.set(m, (revenue.get(m) ?? 0n) + v)
  }

  const incurred = new Map<string, bigint>()
  const cogs = new Map<string, bigint>()
  const balance = new Map<string, bigint>()
  let b = BigInt(wip.opening)
  for (const m of months) {
    const inc = costs.reduce((s, c) => s + (c.values.get(m) ?? 0n), 0n)
    const t = transfer.get(m) ?? 0n
    incurred.set(m, inc)
    cogs.set(m, inc + t)
    b -= t
    balance.set(m, b)
  }
  return { months, costs, incurred, transfer, cogs, balance, revenue }
}

/** 計上月の知らせ（保存は止めない）。docs/plan.md「2.28」 */
export function wipNotices(table: WipTable, recognitionMonth: string | null): string[] {
  if (!recognitionMonth) return ['計上月（検収月）を入れてください。入れるまで、原価は発生した月の費用のまま計上されます。']
  const out: string[] = []
  const other = table.months.filter((m) => m !== recognitionMonth && (table.revenue.get(m) ?? 0n) !== 0n)
  if (other.length > 0) out.push(`計上月以外の月に売上が入っています（${other.map(monthLabel).join('・')}）。検収月に一括の施策の売上は計上月に入れます。`)
  const i = table.months.indexOf(recognitionMonth)
  const before = i > 0 ? (table.balance.get(table.months[i - 1]) ?? 0n) : 0n
  const sales = table.revenue.get(recognitionMonth) ?? 0n
  if (i > 0 && before > sales) out.push('計上月の前月末の仕掛残高が、計上月の売上を超えています（赤字のおそれ）。')
  return out
}

export function monthLabel(m: string): string {
  return `${m.slice(0, 4)}年${Number(m.slice(5, 7))}月`
}
