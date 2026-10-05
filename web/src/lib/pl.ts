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

/** N 系列の P/L の1行。values[i] は系列 i の月 → 金額 */
export type SeriesPlNode = {
  id: string
  label: string
  sub?: string
  measure: SubjectCategory | 'profit'
  children: SeriesPlNode[]
  values: Map<string, bigint>[]
}

function sumSeries(id: string, label: string, measure: SeriesPlNode['measure'], children: SeriesPlNode[], n: number): SeriesPlNode {
  const node: SeriesPlNode = { id, label, measure, children, values: Array.from({ length: n }, () => new Map()) }
  for (const c of children) c.values.forEach((v, i) => addInto(node.values[i], v))
  return node
}

/**
 * 複数のシナリオの金額（数値入力 API の amounts）から P/L を組み立てる。
 * 収益・費用 → 科目 → 内訳（内訳のある科目は、内訳と「その他」＝科目への直接入力）の木と、利益の行を返す。
 * 科目・内訳は、どれかの系列にあれば行にする。
 */
export function buildSeriesPl(series: AmountRow[][]): SeriesPlNode[] {
  const n = series.length
  const subjects = new Map<number, { row: AmountRow; rows: (AmountRow | undefined)[] }>()
  series.forEach((rows, i) => {
    for (const r of rows) {
      const s = subjects.get(r.subject_id) ?? { row: r, rows: Array(n).fill(undefined) }
      s.rows[i] = r
      subjects.set(r.subject_id, s)
    }
  })

  const subjectNode = ({ row, rows }: { row: AmountRow; rows: (AmountRow | undefined)[] }): SeriesPlNode => {
    const id = `s:${row.subject_id}`
    const lineIds = [...new Set(rows.flatMap((r) => r?.lines ?? []).map((x) => x.id))]
    const direct: SeriesPlNode = { id: `${id}:0`, label: 'その他', sub: '科目への直接入力', measure: row.category, children: [], values: rows.map((r) => toMap(r?.values)) }
    if (lineIds.length === 0) return { ...direct, id, label: row.name, sub: row.code }
    const lines: SeriesPlNode[] = lineIds.map((lid) => {
      const ls = rows.map((r) => r?.lines.find((x) => x.id === lid))
      return { id: `${id}:${lid}`, label: ls.find(Boolean)!.name, measure: row.category, children: [], values: ls.map((l) => toMap(l?.values)) }
    })
    if (direct.values.some((v) => v.size > 0)) lines.push(direct)
    return { ...sumSeries(id, row.name, row.category, lines, n), sub: row.code }
  }

  const all = [...subjects.values()].map(subjectNode)
  const revenue = sumSeries('revenue', '収益', 'revenue', all.filter((x) => x.measure === 'revenue'), n)
  const expense = sumSeries('expense', '費用', 'expense', all.filter((x) => x.measure === 'expense'), n)
  const profit: SeriesPlNode = { id: 'profit', label: '利益', sub: '収益 − 費用', measure: 'profit', children: [], values: Array.from({ length: n }, () => new Map()) }
  profit.values.forEach((v, i) => {
    addInto(v, revenue.values[i])
    addInto(v, expense.values[i], -1n)
  })
  return [revenue, expense, profit]
}

/** 2つのシナリオ（基準・最新）の P/L。buildSeriesPl の2系列版 */
export function buildPl(base: AmountRow[], latest: AmountRow[]): PlNode[] {
  const convert = (x: SeriesPlNode): PlNode => ({ id: x.id, label: x.label, sub: x.sub, measure: x.measure, children: x.children.map(convert), base: x.values[0], latest: x.values[1] })
  return buildSeriesPl([base, latest]).map(convert)
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
