import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { RiskActivity, RiskReport } from '../api/types.ts'
import { compareDiffOf, compositionKeys, highCertaintyRate, sortActivities, spreadOf, sumComposition, sumMeasures, warningKinds } from './risk.ts'

const pl = (revenue: number, expense = 0) => ({ revenue: String(revenue), expense: String(expense) })
const act = (code: string, o: Partial<RiskActivity> = {}): RiskActivity => ({
  id: code.length,
  code,
  name: code,
  unit_id: 1,
  owner_user_id: null,
  activity_type: 'project',
  status: 'in_progress',
  confidence_level: 'C',
  assumptions: '',
  full: pl(0),
  weighted: pl(0),
  optimistic: pl(0),
  pessimistic: pl(0),
  actual: pl(0),
  revenue_by_level: {},
  compare: null,
  lines: [],
  warnings: { milestones: [], postponed: { count: 0, days: 0 }, downward: null, consecutive: false, accuracy: null },
  warning_count: 0,
  bad_for_level: false,
  conditions: {},
  ...o,
})
const levels: RiskReport['levels'] = [
  { code: 'A', name: '確定', rate: '1', high: true },
  { code: 'B', name: '高', rate: '0.8', high: true },
  { code: 'C', name: '中', rate: '0.5', high: false },
]

test('sumMeasures: 指標ごとに合計する', () => {
  const t = sumMeasures([act('X', { weighted: pl(540, 40) }), act('Y', { weighted: pl(100) })])
  assert.deepEqual(t.weighted, { revenue: 640n, expense: 40n })
})

test('構成と確度の高い見込の割合（ダウンサイドは分母に含めない）', () => {
  const keys = compositionKeys(levels)
  assert.deepEqual(keys, ['actual', 'A', 'B', 'C', 'downside'])
  const comp = sumComposition([act('X', { revenue_by_level: { actual: '300', A: '500', C: '200', downside: '-50' } })], keys)
  assert.equal(comp.get('downside'), -50n)
  assert.equal(highCertaintyRate(comp, levels), 80) // (300 + 500) / 1000
  assert.equal(highCertaintyRate(new Map(), levels), null)
})

test('振れ幅・比較との差・並べ替え', () => {
  const a = act('A1', { optimistic: pl(1200), pessimistic: pl(0), weighted: pl(540), compare: pl(800) })
  const b = act('B1', { optimistic: pl(600), pessimistic: pl(450), weighted: pl(540), bad_for_level: true, warning_count: 1 })
  assert.equal(spreadOf(a), 1200n)
  assert.equal(compareDiffOf(a), -260n)
  assert.equal(compareDiffOf(b), null)
  assert.deepEqual(sortActivities([b, a], 'spread').map((x) => x.code), ['A1', 'B1'])
  assert.deepEqual(sortActivities([a, b], 'warnings').map((x) => x.code), ['B1', 'A1'])
  assert.deepEqual(sortActivities([b, a], 'compare').map((x) => x.code), ['A1', 'B1'])
})

test('warningKinds: 期日が近いだけのマイルストーンは警告に数えない', () => {
  const w = act('W', {
    warnings: { milestones: [{ name: 'm', due_date: '2026-10-10', status: 'in_progress', risk: 'upcoming' }], postponed: { count: 1, days: 7 }, downward: null, consecutive: true, accuracy: 25 },
  })
  assert.deepEqual(warningKinds(w), ['postponed', 'consecutive', 'accuracy'])
})
