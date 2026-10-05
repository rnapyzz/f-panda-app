import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ActivityProgress, NoteStatus } from '../api/types.ts'
import { countByStatus, profitDiff, sortStatuses, sumTotals, totalsByUnit } from './home.ts'

const item = (code: string, status: NoteStatus, current: number, base: number | null, previous: number | null = null): ActivityProgress => ({
  activity_id: code.length,
  code,
  name: code,
  unit_id: 1,
  owner_user_id: null,
  status,
  has_explanation: false,
  explanation: '',
  causes: [],
  is_priority: false,
  is_watched: false,
  last_edited_at: null,
  completed_at: null,
  current: { revenue: String(current), expense: '0' },
  base: base === null ? null : { revenue: String(base), expense: '0' },
  initial: null,
  revised: null,
  previous: previous === null ? null : { revenue: String(previous), expense: '0' },
  accuracy: null,
})

test('profitDiff: 比較がなければ null', () => {
  assert.equal(profitDiff({ revenue: '1000', expense: '300' }, { revenue: '500', expense: '0' }), 200n)
  assert.equal(profitDiff({ revenue: '1000', expense: '0' }, null), null)
})

test('sortStatuses: 未完了が先、同じ状態は基準との差の大きい順', () => {
  const items = [item('A', 'completed', 100, 0), item('B', 'in_progress', 100, 90), item('C', 'not_started', 100, 100), item('D', 'in_progress', 100, 300)]
  assert.deepEqual(
    sortStatuses(items, 'status').map((x) => x.code),
    ['C', 'D', 'B', 'A'],
  )
  assert.deepEqual(
    sortStatuses(items, 'base').map((x) => x.code),
    ['D', 'A', 'B', 'C'],
  )
  assert.deepEqual(
    sortStatuses([item('E', 'in_progress', 100, 0, 90), item('F', 'in_progress', 100, 0, null), item('G', 'in_progress', 100, 0, 0)], 'previous').map((x) => x.code),
    ['G', 'E', 'F'],
  )
})

test('countByStatus', () => {
  assert.deepEqual(countByStatus([item('A', 'completed', 0, 0), item('B', 'completed', 0, 0), item('C', 'not_started', 0, 0)]), { not_started: 1, in_progress: 0, completed: 2 })
})

test('totalsByUnit・sumTotals: ユニットごとの売上・利益と、未完了の数', () => {
  const a = { ...item('A', 'in_progress', 1000, 800, 900), unit_id: 1, initial: { revenue: '700', expense: '100' } }
  const b = { ...item('B', 'completed', 500, 0, 600), unit_id: 1, initial: { revenue: '400', expense: '0' } }
  const c = { ...item('C', 'not_started', 200, 0, 0), unit_id: 2, current: { revenue: '200', expense: '50' }, initial: null }
  const has = { initial: true, revised: false, previous: true }
  const rows = totalsByUnit([a, b, c], has)
  const u1 = rows.find((r) => r.unitId === 1)!
  assert.deepEqual([u1.count, u1.open, u1.current.revenue, u1.initial!.profit, u1.previous!.revenue, u1.revised], [2, 1, 1500n, 1000n, 1500n, null])
  const u2 = rows.find((r) => r.unitId === 2)!
  assert.deepEqual([u2.current.profit, u2.initial!.revenue], [150n, 0n]) // 施策に期初計画がなければ 0
  const total = sumTotals(rows, has)
  assert.deepEqual([total.count, total.open, total.current.profit, total.initial!.profit], [3, 2, 1650n, 1000n])
})
