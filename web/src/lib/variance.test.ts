import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { AmountRow } from '../api/types.ts'
import { isLargeVariance, summarizeVariance } from './variance.ts'

const months = ['2026-04', '2026-05', '2026-06']
const cell = (target_month: string, amount: number) => ({ target_month, amount, source: 'manual' as const, is_provisional: false, provisional_reason: '' })
const row = (subject_id: number, name: string, category: 'revenue' | 'expense', values: [string, number][]): AmountRow => ({
  subject_id,
  code: String(subject_id),
  name,
  category,
  values: values.map(([m, a]) => cell(m, a)),
  lines: [],
})

test('summarizeVariance: 年間の利益の差と、差の大きい科目・月', () => {
  const current = [row(1, '売上', 'revenue', [['2026-04', 1000], ['2026-05', 3000]]), row(2, '外注費', 'expense', [['2026-04', 500]])]
  const compare = [row(1, '売上', 'revenue', [['2026-04', 1000], ['2026-05', 1000]]), row(2, '外注費', 'expense', [['2026-04', 400]]), row(3, '広告費', 'expense', [['2026-06', 50]])]
  const s = summarizeVariance(current, compare, months)
  assert.equal(s.current, 3500n)
  assert.equal(s.compare, 1550n)
  assert.equal(s.diff, 1950n)
  assert.deepEqual(
    s.subjects.map((x) => [x.label, x.diff]),
    [
      ['売上', 2000n],
      ['外注費', 100n],
      ['広告費', -50n],
    ],
  )
  assert.deepEqual(
    s.months.map((x) => [x.label, x.diff]),
    [
      ['5月', 2000n],
      ['4月', -100n],
      ['6月', 50n],
    ],
  )
})

test('isLargeVariance: 10% 以上かつ 10万円以上', () => {
  assert.equal(isLargeVariance(200000n, 1000000n), true)
  assert.equal(isLargeVariance(-200000n, 1000000n), true)
  assert.equal(isLargeVariance(90000n, 100000n), false) // 10万円未満
  assert.equal(isLargeVariance(150000n, 2000000n), false) // 10% 未満
  assert.equal(isLargeVariance(150000n, 0n), true)
})
