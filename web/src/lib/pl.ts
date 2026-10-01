// 施策の P/L（基準シナリオと比較シナリオの金額・差異）の組み立て。金額は BigInt で扱う。

import type { AmountRow, SubjectCategory } from '../api/types'

export type Grain = 'month' | 'quarter' | 'half' | 'year'

export const grainLabels: Record<Grain, string> = {
  month: '月次',
  quarter: '四半期',
  half: '半期',
  year: '通期',
}

/** 表の列（期間） */
export type Period = { key: string; label: string; months: string[] }

/**
 * 年度の月（4月〜3月の順）を期間に分ける。月次・四半期・半期のときは、最後に通期の列を付ける。
 */
export function periodsOf(months: string[], grain: Grain): Period[] {
  const chunk = (size: number, label: (i: number, ms: string[]) => string): Period[] => {
    const out: Period[] = []
    for (let i = 0; i * size < months.length; i++) {
      const ms = months.slice(i * size, (i + 1) * size)
      out.push({ key: `${grain}-${i}`, label: label(i, ms), months: ms })
    }
    return out
  }
  const total: Period = { key: 'total', label: '通期', months }
  switch (grain) {
    case 'month':
      return [...months.map((m) => ({ key: m, label: `${Number(m.slice(5, 7))}月`, months: [m] })), total]
    case 'quarter':
      return [...chunk(3, (i) => `Q${i + 1}`), total]
    case 'half':
      return [...chunk(6, (i) => (i === 0 ? '上期' : '下期')), total]
    case 'year':
      return [total]
  }
}

/** P/L の1行。base・latest は月 → 金額 */
export type PlNode = {
  id: string
  label: string
  sub?: string
  /** 差異の良し悪しの判定に使う（利益は profit） */
  measure: SubjectCategory | 'profit'
  children: PlNode[]
  base: Map<string, bigint>
  latest: Map<string, bigint>
}

type Values = { target_month: string; amount: number | string }[]

function toMap(values: Values | undefined): Map<string, bigint> {
  const m = new Map<string, bigint>()
  for (const v of values ?? []) m.set(v.target_month, (m.get(v.target_month) ?? 0n) + BigInt(v.amount))
  return m
}

function addInto(dst: Map<string, bigint>, src: Map<string, bigint>, sign = 1n) {
  for (const [k, v] of src) dst.set(k, (dst.get(k) ?? 0n) + v * sign)
}

function sumNodes(id: string, label: string, measure: PlNode['measure'], children: PlNode[]): PlNode {
  const node: PlNode = { id, label, measure, children, base: new Map(), latest: new Map() }
  for (const c of children) {
    addInto(node.base, c.base)
    addInto(node.latest, c.latest)
  }
  return node
}

/**
 * 2つのシナリオの金額（数値入力 API の amounts）から P/L を組み立てる。
 * 収益・費用 → 科目 → 内訳（内訳のある科目は、内訳と「その他」＝科目への直接入力）の木と、利益の行を返す。
 * 科目・内訳は、どちらかのシナリオにあれば行にする。
 */
export function buildPl(base: AmountRow[], latest: AmountRow[]): PlNode[] {
  const subjects = new Map<number, { row: AmountRow; base?: AmountRow; latest?: AmountRow }>()
  for (const r of base) subjects.set(r.subject_id, { row: r, base: r })
  for (const r of latest) {
    const s = subjects.get(r.subject_id)
    if (s) s.latest = r
    else subjects.set(r.subject_id, { row: r, latest: r })
  }

  const subjectNode = ({ row, base: b, latest: l }: { row: AmountRow; base?: AmountRow; latest?: AmountRow }): PlNode => {
    const id = `s:${row.subject_id}`
    const lineIds = [...new Set([...(b?.lines ?? []), ...(l?.lines ?? [])].map((x) => x.id))]
    const direct: PlNode = { id: `${id}:0`, label: 'その他', sub: '科目への直接入力', measure: row.category, children: [], base: toMap(b?.values), latest: toMap(l?.values) }
    if (lineIds.length === 0) return { ...direct, id, label: row.name, sub: row.code }
    const lines: PlNode[] = lineIds.map((lid) => {
      const bl = b?.lines.find((x) => x.id === lid)
      const ll = l?.lines.find((x) => x.id === lid)
      return { id: `${id}:${lid}`, label: (ll ?? bl)!.name, measure: row.category, children: [], base: toMap(bl?.values), latest: toMap(ll?.values) }
    })
    if (direct.base.size > 0 || direct.latest.size > 0) lines.push(direct)
    return { ...sumNodes(id, row.name, row.category, lines), sub: row.code }
  }

  const all = [...subjects.values()].map(subjectNode)
  const revenue = sumNodes('revenue', '収益', 'revenue', all.filter((n) => n.measure === 'revenue'))
  const expense = sumNodes('expense', '費用', 'expense', all.filter((n) => n.measure === 'expense'))
  const profit: PlNode = { id: 'profit', label: '利益', sub: '収益 − 費用', measure: 'profit', children: [], base: new Map(), latest: new Map() }
  addInto(profit.base, revenue.base)
  addInto(profit.base, expense.base, -1n)
  addInto(profit.latest, revenue.latest)
  addInto(profit.latest, expense.latest, -1n)
  return [revenue, expense, profit]
}

/** 期間の月の合計 */
export function sumOver(values: Map<string, bigint>, months: string[]): bigint {
  return months.reduce((s, m) => s + (values.get(m) ?? 0n), 0n)
}

/** 年度の既定値: 今の年度（4月始まり）のシナリオがあればそれ、なければ一番新しい年度 */
export function defaultFiscalYear(years: number[], today = new Date()): number | undefined {
  const current = today.getMonth() >= 3 ? today.getFullYear() : today.getFullYear() - 1
  return years.includes(current) ? current : [...years].sort((a, b) => b - a)[0]
}
