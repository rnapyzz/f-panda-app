// 翌期の計画で、施策ごとに今回の見込から値を引き継ぐ（docs/plan.md「2.27」）。
// 画面の表に未保存の入力として入れる値を組み立てる（保存は担当者が確かめてから行う）。

import type { ValuesView } from '../api/types.ts'

export type CarryOver = {
  /** セルのキー（a:科目:内訳(0は直接入力):月 / d:ドライバー:月）→ 値 */
  cells: Map<string, string>
  /** 直接入力の金額を入れる科目（表に行がなければ足す） */
  subjects: number[]
  /** beyond: 今回の見込の翌年度の月の値をそのまま / march: 3月の値を12か月に / none: 引き継ぐ値がない */
  source: 'beyond' | 'march' | 'none'
}

/**
 * 今回の見込（current。翌期の計画の前の年度の版）から、翌期の計画の月（planMonths）に入れる値を作る。
 * 翌年度の月（docs/plan.md「2.26」）の値があればそのまま、なければ最後の月（3月）の値を12か月に入れる。
 * 計算式で反映する内訳は入れない（ドライバー値から試算する）。0 の値は入れない。
 */
export function carryOver(current: ValuesView, planMonths: string[]): CarryOver {
  type Source = { key: (m: string) => string; values: { target_month: string; value: string }[]; subject?: number }
  const sources: Source[] = []
  for (const r of current.amounts) {
    sources.push({ key: (m) => `a:${r.subject_id}:0:${m}`, values: r.values.map((c) => ({ target_month: c.target_month, value: String(c.amount) })), subject: r.subject_id })
    for (const l of r.lines.filter((l) => !l.formula_enabled)) {
      sources.push({ key: (m) => `a:${r.subject_id}:${l.id}:${m}`, values: l.values.map((c) => ({ target_month: c.target_month, value: String(c.amount) })) })
    }
  }
  for (const d of current.drivers) {
    sources.push({ key: (m) => `d:${d.id}:${m}`, values: d.values.map((c) => ({ target_month: c.target_month, value: String(c.value) })) })
  }
  const nonZero = (v: string) => v !== '' && Number(v) !== 0
  const plan = new Set(planMonths)
  const beyond = sources.some((s) => s.values.some((c) => plan.has(c.target_month) && nonZero(c.value)))
  const last = current.months[current.months.length - 1]
  const cells = new Map<string, string>()
  const subjects = new Set<number>()
  for (const s of sources) {
    const picked = beyond
      ? s.values.filter((c) => plan.has(c.target_month) && nonZero(c.value)).map((c) => [c.target_month, c.value] as const)
      : s.values.filter((c) => c.target_month === last && nonZero(c.value)).flatMap((c) => planMonths.map((m) => [m, c.value] as const))
    for (const [m, value] of picked) {
      cells.set(s.key(m), value)
      if (s.subject !== undefined) subjects.add(s.subject)
    }
  }
  return { cells, subjects: [...subjects], source: cells.size === 0 ? 'none' : beyond ? 'beyond' : 'march' }
}
