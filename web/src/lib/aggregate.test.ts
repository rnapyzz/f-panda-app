import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ReportRow, SubjectCategory } from '../api/types'
import { aggregate, isFavorable, measureOf, varianceRate } from './aggregate.ts'

// 科目 1 = 収益、2 = 費用
const categoryOf = (id: number): SubjectCategory | undefined => (id === 1 ? 'revenue' : id === 2 ? 'expense' : undefined)
const row = (function_id: number, subject_id: number, month: string, values: Record<string, string>): ReportRow => ({ function_id, subject_id, month, values })

const rows = [
  row(10, 1, '2026-04', { s1: '1000', s2: '1200' }),
  row(10, 2, '2026-04', { s1: '300' }),
  row(10, 1, '2026-10', { s1: '2000', s2: '2500' }),
  row(20, 1, '2026-04', { s1: '99999999999999999' }), // 別の機能（Number では桁落ちする大きさ）
  row(10, 9, '2026-04', { s1: '5' }), // 区分不明の科目は無視
]

test('aggregate: 系列ごとに収益・費用・科目別・月別を集計する', () => {
  const all = new Set(['2026-04', '2026-10'])
  const t = aggregate(rows, ['s1', 's2'], categoryOf, (r) => r.function_id === 10, all)
  const s1 = t.get('s1')!
  assert.equal(s1.revenue, 3000n)
  assert.equal(s1.expense, 300n)
  assert.equal(measureOf(s1, 'profit'), 2700n)
  assert.equal(s1.bySubject.get(1), 3000n)
  assert.deepEqual(s1.byMonth.get('2026-10'), { revenue: 2000n, expense: 0n })
  assert.equal(t.get('s2')!.revenue, 3700n)
})

test('aggregate: 期間の絞り込みは合計にだけ効き、月別は全月を持つ', () => {
  const t = aggregate(rows, ['s1'], categoryOf, (r) => r.function_id === 10, new Set(['2026-10']))
  const s1 = t.get('s1')!
  assert.equal(s1.revenue, 2000n)
  assert.equal(s1.expense, 0n)
  assert.equal(s1.byMonth.get('2026-04')!.revenue, 1000n)
})

test('aggregate: 大きな金額も誤差なく合計する', () => {
  const t = aggregate(rows, ['s1'], categoryOf, () => true, new Set(['2026-04', '2026-10']))
  assert.equal(t.get('s1')!.revenue, 99999999999999999n + 3000n)
})

test('varianceRate: 基準との差異率（0.1% 単位）', () => {
  assert.equal(varianceRate(1000n, 1250n), 25)
  assert.equal(varianceRate(3000n, 2900n), -3.3)
  assert.equal(varianceRate(-1000n, -500n), 50) // 基準がマイナスでも増えればプラス
  assert.equal(varianceRate(0n, 100n), null)
})

test('isFavorable: 収益・利益は増えると有利、費用は増えると不利', () => {
  assert.equal(isFavorable(100n, 'revenue'), true)
  assert.equal(isFavorable(100n, 'profit'), true)
  assert.equal(isFavorable(100n, 'expense'), false)
  assert.equal(isFavorable(-100n, 'expense'), true)
  assert.equal(isFavorable(0n, 'revenue'), null)
})
