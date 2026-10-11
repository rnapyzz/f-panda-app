import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ValuesView } from '../api/types.ts'
import { carryOver } from './carryover.ts'

const months = (fy: number) => Array.from({ length: 12 }, (_, i) => (i < 9 ? `${fy}-${String(i + 4).padStart(2, '0')}` : `${fy + 1}-${String(i - 8).padStart(2, '0')}`))
const cell = (m: string, amount: number) => ({ target_month: m, amount, source: 'manual' as const, is_provisional: false, provisional_reason: '' })
const view = (amounts: ValuesView['amounts'], drivers: ValuesView['drivers'] = []) => ({ months: months(2026), beyond_months: months(2027), amounts, drivers }) as unknown as ValuesView
const line = (id: number, formula: boolean, values: ReturnType<typeof cell>[]) => ({ id, name: `L${id}`, expression: '', formula_enabled: formula, confidence_level: null, outlook: 'base' as const, values })

test('carryOver: 翌年度の月の値がなければ、3月の値を12か月に（計算式の内訳と 0 は入れない）', () => {
  const v = view(
    [{ subject_id: 1, code: 'R', name: '売上', category: 'revenue', values: [cell('2027-02', 5), cell('2027-03', 100)], lines: [line(7, false, [cell('2027-03', 30)]), line(8, true, [cell('2027-03', 999)])] }],
    [{ id: 3, code: 'p', name: '単価', driver_kind: 'value', unit: '', values: [{ target_month: '2027-03', value: '1000', is_provisional: false, provisional_reason: '' }] }],
  )
  const c = carryOver(v, months(2027))
  assert.equal(c.source, 'march')
  assert.equal(c.cells.get('a:1:0:2027-04'), '100')
  assert.equal(c.cells.get('a:1:0:2028-03'), '100')
  assert.equal(c.cells.get('a:1:7:2027-10'), '30')
  assert.equal(c.cells.get('d:3:2027-06'), '1000')
  assert.equal([...c.cells.keys()].some((k) => k.startsWith('a:1:8:')), false)
  assert.equal(c.cells.size, 36)
  assert.deepEqual(c.subjects, [1])
})

test('carryOver: 翌年度の月の値があれば、そのまま入れる', () => {
  const v = view([{ subject_id: 1, code: 'R', name: '売上', category: 'revenue', values: [cell('2027-03', 100), cell('2027-06', 2000)], lines: [] }])
  const c = carryOver(v, months(2027))
  assert.equal(c.source, 'beyond')
  assert.deepEqual([...c.cells.entries()], [['a:1:0:2027-06', '2000']])
})

test('carryOver: 値がなければ none', () => {
  assert.equal(carryOver(view([]), months(2027)).source, 'none')
})
