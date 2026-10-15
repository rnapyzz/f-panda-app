import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ReportRow, Subject } from '../api/types.ts'
import type { DashActivity } from './dashboard.ts'
import { halves, newVsContinuing, plStatement, quarters, revenueByActivity, revenueByType, shareRate, summarySentences, topRate, topShares } from './overview.ts'

const months = ['2026-04', '2026-05', '2026-06', '2026-07', '2026-08', '2026-09', '2026-10', '2026-11', '2026-12', '2027-01', '2027-02', '2027-03']
const subject = (id: number, name: string, category: Subject['category'], sort_order = id) => ({ id, code: `S${id}`, name, category, sort_order }) as Subject
const subjects = [subject(1, '売上', 'revenue'), subject(2, '受託売上', 'revenue'), subject(3, '外注費', 'expense'), subject(4, '家賃', 'expense')]
const row = (activity_id: number, subject_id: number, month: string, v: number): ReportRow => ({ unit_id: 10, activity_id, subject_id, month, values: { s: String(v) } })
const rows = [
  row(1, 1, '2026-04', 100),
  row(1, 1, '2026-10', 300),
  row(2, 2, '2026-07', 200),
  row(2, 3, '2026-07', 50),
  row(3, 3, '2027-01', 30),
]

test('quarters: 実績の月を含む四半期に印', () => {
  const q = quarters(months, '2026-05')
  assert.deepEqual(q.map((x) => [x.label, x.months[0], x.partlyActual]), [
    ['Q1', '2026-04', true],
    ['Q2', '2026-07', false],
    ['Q3', '2026-10', false],
    ['Q4', '2027-01', false],
  ])
})

test('plStatement: 科目 × 四半期・年間、計・利益・利益率。金額のない科目は出さない', () => {
  const { lines } = plStatement(rows, 's', subjects, months, null)
  assert.deepEqual(
    lines.map((l) => [l.label, l.values.get('q1'), l.values.get('q3'), l.values.get('year')]),
    [
      ['売上', 100n, 300n, 400n],
      ['受託売上', 0n, 0n, 200n],
      ['収益の計', 100n, 300n, 600n],
      ['外注費', 0n, 0n, 80n],
      ['費用の計', 0n, 0n, 80n],
      ['利益', 100n, 300n, 520n],
      ['利益率', 100, 100, 86.6],
    ],
  )
  // Q4 は売上がないので利益率は null
  assert.equal(lines.find((l) => l.key === 'margin')!.values.get('q4'), null)
})

test('topShares: 大きい順、上限を超えたらその他、0 以下は除く', () => {
  const s = (key: string, v: number) => ({ key, label: key, value: BigInt(v) })
  assert.deepEqual(
    topShares([s('a', 5), s('b', 50), s('c', 0), s('d', 20), s('e', 10)], 3).map((x) => [x.label, x.value]),
    [
      ['b', 50n],
      ['d', 20n],
      ['その他（2件）', 15n],
    ],
  )
})

test('halves・shareRate: 上期・下期の売上と割合', () => {
  const byMonth = new Map([
    ['2026-04', { revenue: 100n, expense: 0n }],
    ['2026-10', { revenue: 300n, expense: 0n }],
  ])
  const h = halves(byMonth, months)
  assert.deepEqual(h, { first: 100n, second: 300n })
  assert.equal(shareRate(h.second, h.first + h.second), 75)
  assert.equal(shareRate(1n, 0n), null)
})

test('施策ごと・タイプ別・継続と新規・上位の割合', () => {
  const rev = revenueByActivity(rows, 's', (id) => subjects.find((x) => x.id === id)?.category, months)
  assert.deepEqual([...rev], [
    [1, 400n],
    [2, 200n],
    [3, 0n],
  ])
  const act = (id: number, activity_type: DashActivity['activity_type']) => ({ id, code: '', name: '', unit_id: 10, owner_user_id: null, activity_type, confidence_level: 'A', causes: [] })
  const byType = revenueByType(rev, [act(1, 'recurring'), act(2, 'project'), act(3, 'cost_pool')])
  assert.equal(byType.get('recurring'), 400n)
  assert.equal(byType.get('project'), 200n)
  // 年度の初日に始まる施策は継続として数える（年度の途中で始まる施策だけを新規に）
  const starts: Record<number, string | null> = { 1: '2026-04-01', 2: '2026-06-01', 3: null }
  assert.deepEqual(newVsContinuing(rev, (id) => starts[id], '2026-04-01'), { new: 200n, continuing: 400n })
  assert.equal(topRate(rev, 1), 66.6)
  assert.equal(topRate(new Map(), 5), null)
})

test('summarySentences: 値がないものは出さない', () => {
  const fmt = (v: bigint) => String(v)
  assert.deepEqual(summarySentences({ revenue: 600n, profit: 520n, topType: { label: '運用型', rate: 66.6 }, top5: 100, sure: 78, secondHalf: 50, newRate: 33.3 }, fmt), [
    '売上 600・利益 520（利益率 86.6%）。',
    '売上の 66.6% が運用型、上位 5 施策で 100%。',
    '確度の高い段階と実績で 78%。下期に売上の 50%。年度の途中で始まる施策の売上は 33.3%。',
  ])
  assert.deepEqual(summarySentences({ revenue: 0n, profit: -10n, topType: null, top5: null, sure: null, secondHalf: null, newRate: null }, fmt), ['売上 0・利益 -10。'])
})
