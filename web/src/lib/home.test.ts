import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ActivityProgress, NoteStatus } from '../api/types.ts'
import { countByStatus, profitDiff, sortStatuses } from './home.ts'

const item = (code: string, status: NoteStatus, current: number, base: number | null, previous: number | null = null): ActivityProgress => ({
  activity_id: code.length,
  code,
  name: code,
  unit_id: 1,
  owner_user_id: null,
  status,
  has_explanation: false,
  last_edited_at: null,
  completed_at: null,
  current: { revenue: String(current), expense: '0' },
  base: base === null ? null : { revenue: String(base), expense: '0' },
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
