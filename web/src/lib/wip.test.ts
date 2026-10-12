import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ValuesView } from '../api/types.ts'
import { buildWip, wipNotices } from './wip.ts'

const months = (fy: number) => Array.from({ length: 12 }, (_, i) => (i < 9 ? `${fy}-${String(i + 4).padStart(2, '0')}` : `${fy + 1}-${String(i - 8).padStart(2, '0')}`))
const cell = (m: string, amount: number) => ({ target_month: m, amount, source: 'manual' as const, is_provisional: false, provisional_reason: '' })
const row = (subject_id: number, name: string, category: 'revenue' | 'expense', values: ReturnType<typeof cell>[]) => ({ subject_id, code: String(subject_id), name, category, values, lines: [] })
const span = (from: number, to: number, amount: number) => months(2026).slice(from, to + 1).map((m) => cell(m, amount))

// 4月着手・9月検収、4〜10月に人件費 100・外注費 50、9月に売上 1,200（docs/plan.md「2.28」の例）
const view = (recognition: string | null, opening = 0) =>
  ({
    months: months(2026),
    beyond_months: [],
    wip: { transfer_subject_id: 9, cost_subject_ids: [1, 2], recognition_month: recognition, opening },
    amounts: [
      row(5, '売上', 'revenue', [cell('2026-09', 1200)]),
      row(1, '人件費', 'expense', span(0, 6, 100)),
      row(2, '外注費', 'expense', span(0, 6, 50)),
      row(9, '仕掛品振替', 'expense', [...span(0, 4, -150), cell('2026-09', 750)]),
    ],
  }) as unknown as ValuesView

test('buildWip: 発生原価・振替・売上原価・月末仕掛残高', () => {
  const t = buildWip(view('2026-09'))!
  assert.equal(t.costs.length, 2)
  assert.equal(t.incurred.get('2026-04'), 150n)
  assert.equal(t.cogs.get('2026-04'), 0n)
  assert.equal(t.balance.get('2026-04'), 150n)
  assert.equal(t.balance.get('2026-08'), 750n)
  assert.equal(t.cogs.get('2026-09'), 900n)
  assert.equal(t.balance.get('2026-09'), 0n)
  assert.equal(t.cogs.get('2026-10'), 150n)
  assert.equal(t.balance.get('2027-03'), 0n)
})

test('buildWip: 前の年度からの仕掛残高（opening）を引き継ぐ', () => {
  const t = buildWip(view('2026-09', 300))!
  assert.equal(t.balance.get('2026-04'), 450n)
  assert.equal(t.balance.get('2026-09'), 300n) // この例の振替は 750 だけを戻すので、引き継いだ 300 が残る
})

test('buildWip: 検収月に一括でない施策は null', () => {
  assert.equal(buildWip({ ...view('2026-09'), wip: null }), null)
})

test('wipNotices: 計上月なし・計上月以外の売上・赤字のおそれ', () => {
  assert.match(wipNotices(buildWip(view(null))!, null)[0], /計上月（検収月）を入れてください/)
  assert.deepEqual(wipNotices(buildWip(view('2026-09'))!, '2026-09'), [])
  const other = wipNotices(buildWip(view('2026-10'))!, '2026-10')
  assert.match(other[0], /2026年9月/)
  const v = view('2026-09')
  v.amounts[0].values = [cell('2026-09', 500)]
  assert.match(wipNotices(buildWip(v)!, '2026-09')[0], /赤字のおそれ/)
})
