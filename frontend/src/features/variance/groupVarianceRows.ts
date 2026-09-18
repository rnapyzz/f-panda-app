import type { VarianceRow } from '../../api/facts'

export interface VarianceGroup {
  key: string
  business_id: number
  business_name: string
  department_id: number
  department_name: string
  account_id: number
  account_name: string
  budget_amount: number | null
  forecast_amount: number | null
  actual_amount: number | null
  variance_amount: number | null
  rows: VarianceRow[]
}

function addNullable(acc: number | null, v: number | null): number | null {
  if (v === null) return acc
  return (acc ?? 0) + v
}

// Aggregates period-level variance rows up to one row per (business,
// department, account) — the "集計" (roll-up) that the detail rows in
// `rows` can then be drilled down into. A dimension's summed amount stays
// null only when every contributing period is null for it, so "no budget
// entered at all" still reads as "—" rather than a misleading 0.
export function groupVarianceRowsByDimension(rows: VarianceRow[]): VarianceGroup[] {
  const groups = new Map<string, VarianceGroup>()

  for (const r of rows) {
    const key = `${r.business_id}-${r.department_id}-${r.account_id}`
    let g = groups.get(key)
    if (!g) {
      g = {
        key,
        business_id: r.business_id,
        business_name: r.business_name,
        department_id: r.department_id,
        department_name: r.department_name,
        account_id: r.account_id,
        account_name: r.account_name,
        budget_amount: null,
        forecast_amount: null,
        actual_amount: null,
        variance_amount: null,
        rows: [],
      }
      groups.set(key, g)
    }
    g.rows.push(r)
    g.budget_amount = addNullable(g.budget_amount, r.budget_amount)
    g.forecast_amount = addNullable(g.forecast_amount, r.forecast_amount)
    g.actual_amount = addNullable(g.actual_amount, r.actual_amount)
  }

  const result = Array.from(groups.values())
  for (const g of result) {
    g.rows.sort((a, b) => a.fiscal_month - b.fiscal_month)
    if (g.budget_amount !== null && g.actual_amount !== null) {
      g.variance_amount = g.actual_amount - g.budget_amount
    }
  }
  result.sort(
    (a, b) =>
      a.business_name.localeCompare(b.business_name) ||
      a.department_name.localeCompare(b.department_name) ||
      a.account_name.localeCompare(b.account_name),
  )
  return result
}
