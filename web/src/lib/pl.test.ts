import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { AmountRow } from '../api/types.ts'
import { buildPl, buildSeriesPl, defaultFiscalYear, periodsOf, sumOver } from './pl.ts'

const months = ['2026-04', '2026-05', '2026-06', '2026-07', '2026-08', '2026-09', '2026-10', '2026-11', '2026-12', '2027-01', '2027-02', '2027-03']

const cell = (target_month: string, amount: number) => ({ target_month, amount, source: 'manual' as const, is_provisional: false, provisional_reason: '' })

test('periodsOf: 月次・四半期・半期は通期の列が付く', () => {
  assert.deepEqual(
    periodsOf(months, 'quarter').map((p) => [p.label, p.months.length]),
    [
      ['Q1', 3],
      ['Q2', 3],
      ['Q3', 3],
      ['Q4', 3],
      ['通期', 12],
    ],
  )
  assert.deepEqual(
    periodsOf(months, 'half').map((p) => p.label),
    ['上期', '下期', '通期'],
  )
  assert.equal(periodsOf(months, 'month').length, 13)
  assert.equal(periodsOf(months, 'month')[9].label, '1月')
  assert.deepEqual(
    periodsOf(months, 'year').map((p) => p.label),
    ['通期'],
  )
})

test('buildPl: 収益・費用 → 科目 → 内訳と「その他」、利益', () => {
  const base: AmountRow[] = [
    {
      subject_id: 1,
      code: '4110',
      name: '売上',
      category: 'revenue',
      values: [cell('2026-04', 1000)],
      lines: [{ id: 10, name: '月額利用料', expression: 'a * b', formula_enabled: true, confidence_level: null, outlook: 'base' as const, values: [cell('2026-04', 5000), cell('2026-05', 5000)] }],
    },
    { subject_id: 2, code: '8110', name: '外注費', category: 'expense', values: [cell('2026-04', 3000)], lines: [] },
  ]
  // 比較側にだけある内訳・科目も行になる
  const latest: AmountRow[] = [
    {
      subject_id: 1,
      code: '4110',
      name: '売上',
      category: 'revenue',
      values: [],
      lines: [
        { id: 10, name: '月額利用料', expression: 'a * b', formula_enabled: true, confidence_level: null, outlook: 'base' as const, values: [cell('2026-04', 6000)] },
        { id: 11, name: '初期費用', expression: '', formula_enabled: false, confidence_level: null, outlook: 'base' as const, values: [cell('2026-04', 200)] },
      ],
    },
    { subject_id: 3, code: '8120', name: '広告費', category: 'expense', values: [cell('2026-05', 400)], lines: [] },
  ]
  const [revenue, expense, profit] = buildPl(base, latest)

  assert.equal(revenue.label, '収益')
  assert.equal(sumOver(revenue.base, months), 11000n)
  assert.equal(sumOver(revenue.latest, months), 6200n)
  const sales = revenue.children[0]
  assert.deepEqual(
    sales.children.map((c) => c.label),
    ['月額利用料', '初期費用', 'その他'],
  )
  assert.equal(sumOver(sales.children[2].base, months), 1000n)

  assert.deepEqual(
    expense.children.map((c) => [c.label, c.children.length]),
    [
      ['外注費', 0],
      ['広告費', 0],
    ],
  )
  assert.equal(sumOver(profit.base, months), 8000n)
  assert.equal(sumOver(profit.latest, months), 5800n)
  assert.equal(sumOver(profit.latest, ['2026-05']), -400n)
})

test('buildPl: 内訳のある科目で「その他」が空なら行にしない', () => {
  const rows: AmountRow[] = [{ subject_id: 1, code: '4110', name: '売上', category: 'revenue', values: [], lines: [{ id: 10, name: 'x', expression: '', formula_enabled: false, confidence_level: null, outlook: 'base' as const, values: [] }] }]
  assert.deepEqual(
    buildPl(rows, [])[0].children[0].children.map((c) => c.label),
    ['x'],
  )
})

test('defaultFiscalYear: 4月始まりの今の年度、なければ最新', () => {
  assert.equal(defaultFiscalYear([2025, 2026], new Date(2026, 9, 1)), 2026)
  assert.equal(defaultFiscalYear([2025, 2026], new Date(2026, 2, 1)), 2025)
  assert.equal(defaultFiscalYear([2024, 2027], new Date(2026, 9, 1)), 2027)
  assert.equal(defaultFiscalYear([], new Date(2026, 9, 1)), undefined)
})

test('buildSeriesPl: 3つの系列（今回・基準・前回見込）を同じ木に並べる', () => {
  const row = (amount: number, month = '2026-04'): AmountRow => ({ subject_id: 1, code: '4110', name: '売上', category: 'revenue', values: [cell(month, amount)], lines: [] })
  const cost = (amount: number): AmountRow => ({ subject_id: 2, code: '8110', name: '外注費', category: 'expense', values: [cell('2026-04', amount)], lines: [] })
  const [revenue, , profit] = buildSeriesPl([[row(1200), cost(300)], [row(1000)], []])
  assert.deepEqual(
    revenue.values.map((v) => sumOver(v, months)),
    [1200n, 1000n, 0n],
  )
  assert.deepEqual(
    profit.values.map((v) => sumOver(v, months)),
    [900n, 1000n, 0n],
  )
  assert.equal(revenue.children[0].values.length, 3)
})
