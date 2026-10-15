// シナリオの全体像（docs/plan.md「2.31」）の集計。比較はせず、1つのシナリオの金額の大きさと構成を出す。金額は BigInt で扱う。
// 金額の行は予実比較 API の施策ごとの行（lib/dashboard.ts の scopedRows で範囲を絞ったもの）。

import type { ActivityType, ReportRow, Subject, SubjectCategory } from '../api/types.ts'
import { aggregate } from './aggregate.ts'
import { profitOf, totalsByActivity, totalsBySeries, type DashActivity, type Group, type PL } from './dashboard.ts'

/** 割合（%、小数点以下1桁）。全体が 0 以下なら null */
export function shareRate(part: bigint, total: bigint): number | null {
  if (total <= 0n) return null
  return Number((part * 1000n) / total) / 10
}

// --- ① 年間の P/L ---

export type Quarter = { key: string; label: string; months: string[]; partlyActual: boolean }

/** 年度の12か月を四半期に分ける。実績の月（決算確定月以前）を含む四半期に印を付ける */
export function quarters(months: string[], actualThrough: string | null): Quarter[] {
  return [0, 1, 2, 3].map((i) => {
    const ms = months.slice(i * 3, i * 3 + 3)
    return { key: `q${i + 1}`, label: `Q${i + 1}`, months: ms, partlyActual: actualThrough !== null && ms.some((m) => m <= actualThrough) }
  })
}

export type PlLine = {
  key: string
  label: string
  kind: 'subject' | 'total' | 'profit' | 'margin'
  category?: SubjectCategory
  /** 四半期のキー（q1〜q4）と year → 金額（利益率の行は %。売上がなければ null） */
  values: Map<string, bigint | number | null>
}

/**
 * 科目 × 四半期・年間の P/L。行は 収益の科目 → 収益の計 → 費用の科目 → 費用の計 → 利益 → 利益率。
 * 科目は subjects の順（区分・表示順）で、年間の金額がある科目だけ。
 */
export function plStatement(rows: ReportRow[], key: string, subjects: Subject[], months: string[], actualThrough: string | null): { quarters: Quarter[]; lines: PlLine[] } {
  const qs = quarters(months, actualThrough)
  const categoryOf = (id: number) => subjects.find((s) => s.id === id)?.category
  const periods = [...qs.map((q) => ({ key: q.key, months: q.months })), { key: 'year', months }]
  const bySubject = new Map<string, Map<number, bigint>>()
  const totals = new Map<string, PL>()
  for (const p of periods) {
    const t = aggregate(rows, [key], categoryOf, () => true, new Set(p.months)).get(key)!
    bySubject.set(p.key, t.bySubject)
    totals.set(p.key, { revenue: t.revenue, expense: t.expense })
  }
  const year = bySubject.get('year')!
  const lines: PlLine[] = []
  for (const category of ['revenue', 'expense'] as SubjectCategory[]) {
    for (const s of subjects.filter((x) => x.category === category && (year.get(x.id) ?? 0n) !== 0n)) {
      lines.push({ key: `s${s.id}`, label: s.name, kind: 'subject', category, values: new Map(periods.map((p) => [p.key, bySubject.get(p.key)!.get(s.id) ?? 0n])) })
    }
    lines.push({ key: category, label: category === 'revenue' ? '収益の計' : '費用の計', kind: 'total', category, values: new Map(periods.map((p) => [p.key, totals.get(p.key)![category]])) })
  }
  lines.push({ key: 'profit', label: '利益', kind: 'profit', values: new Map(periods.map((p) => [p.key, profitOf(totals.get(p.key)!)])) })
  lines.push({ key: 'margin', label: '利益率', kind: 'margin', values: new Map(periods.map((p) => [p.key, shareRate(profitOf(totals.get(p.key)!), totals.get(p.key)!.revenue)])) })
  return { quarters: qs, lines }
}

// --- ② 何でできているか ---

export type Share = { key: string; label: string; value: bigint; group?: Group }

/** 大きい順に並べ、limit を超えた分は「その他」にまとめる（0 以下は除く） */
export function topShares(items: Share[], limit = 8): Share[] {
  const sorted = items.filter((x) => x.value > 0n).sort((a, b) => (a.value === b.value ? a.label.localeCompare(b.label, 'ja') : a.value > b.value ? -1 : 1))
  if (sorted.length <= limit) return sorted
  const rest = sorted.slice(limit - 1)
  return [...sorted.slice(0, limit - 1), { key: 'other', label: `その他（${rest.length}件）`, value: rest.reduce((s, x) => s + x.value, 0n) }]
}

/** 区分ごとの年間の売上・利益 */
export function groupTotals(
  groups: Group[],
  rows: ReportRow[],
  includeOf: (g: Group) => (row: ReportRow) => boolean,
  key: string,
  categoryOf: (subjectId: number) => SubjectCategory | undefined,
  months: string[],
): { group: Group; pl: PL }[] {
  return groups.map((g) => ({ group: g, pl: totalsBySeries(rows, [key], categoryOf, months, includeOf(g)).get(key)!.year }))
}

// --- ③ 1年の形 ---

/** 上期（4〜9月）・下期（10〜3月）の売上 */
export function halves(byMonth: Map<string, PL>, months: string[]): { first: bigint; second: bigint } {
  const sum = (ms: string[]) => ms.reduce((s, m) => s + (byMonth.get(m)?.revenue ?? 0n), 0n)
  return { first: sum(months.slice(0, 6)), second: sum(months.slice(6)) }
}

// --- ④ 計画の性質 ---

/** 施策ごとの年間の売上（未割当は 0 番） */
export function revenueByActivity(rows: ReportRow[], key: string, categoryOf: (subjectId: number) => SubjectCategory | undefined, months: string[]): Map<number, bigint> {
  return new Map([...totalsByActivity(rows, [key], categoryOf, months)].map(([id, s]) => [id, s.get(key)!.revenue]))
}

export const typeOrder: ActivityType[] = ['recurring', 'project', 'cost_pool']

/** 施策のタイプごとの売上 */
export function revenueByType(revenue: Map<number, bigint>, activities: DashActivity[]): Map<ActivityType, bigint> {
  const out = new Map<ActivityType, bigint>(typeOrder.map((t) => [t, 0n]))
  const byId = new Map(activities.map((a) => [a.id, a]))
  for (const [id, v] of revenue) {
    const t = byId.get(id)?.activity_type
    if (t) out.set(t, out.get(t)! + v)
  }
  return out
}

/** 年度の途中で始まる施策（開始日が年度の初日より後）と、それ以外（継続・年度の初めから）の施策の売上 */
export function newVsContinuing(revenue: Map<number, bigint>, startOf: (activityId: number) => string | null | undefined, fiscalYearStart: string): { new: bigint; continuing: bigint } {
  const out = { new: 0n, continuing: 0n }
  for (const [id, v] of revenue) {
    if (id === 0) continue
    const start = startOf(id)
    if (start && start > fiscalYearStart) out.new += v
    else out.continuing += v
  }
  return out
}

/** 売上の上位 n 施策の割合（%） */
export function topRate(revenue: Map<number, bigint>, n: number): number | null {
  const values = [...revenue.entries()].filter(([id, v]) => id !== 0 && v > 0n).map(([, v]) => v)
  const total = values.reduce((s, v) => s + v, 0n)
  const top = values.sort((a, b) => (a === b ? 0 : a > b ? -1 : 1)).slice(0, n).reduce((s, v) => s + v, 0n)
  return shareRate(top, total)
}

// --- ひとことで ---

export type SummaryInput = {
  revenue: bigint
  profit: bigint
  /** いちばん大きいタイプの名前と割合 */
  topType: { label: string; rate: number } | null
  top5: number | null
  /** 確度の高い段階と実績の割合 */
  sure: number | null
  secondHalf: number | null
  newRate: number | null
}

/** 全体像の要約の文（値がないものは出さない）。fmt は金額の書式（画面の単位） */
export function summarySentences(x: SummaryInput, fmt: (v: bigint) => string): string[] {
  const out: string[] = []
  const margin = shareRate(x.profit, x.revenue)
  out.push(`売上 ${fmt(x.revenue)}・利益 ${fmt(x.profit)}${margin === null ? '' : `（利益率 ${margin}%）`}。`)
  const mix: string[] = []
  if (x.topType) mix.push(`売上の ${x.topType.rate}% が${x.topType.label}`)
  if (x.top5 !== null) mix.push(`上位 5 施策で ${x.top5}%`)
  if (mix.length > 0) out.push(`${mix.join('、')}。`)
  const nature: string[] = []
  if (x.sure !== null) nature.push(`確度の高い段階と実績で ${x.sure}%`)
  if (x.secondHalf !== null) nature.push(`下期に売上の ${x.secondHalf}%`)
  if (x.newRate !== null) nature.push(`年度の途中で始まる施策の売上は ${x.newRate}%`)
  if (nature.length > 0) out.push(`${nature.join('。')}。`)
  return out
}
