import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ActivityProgress } from '../api/types.ts'
import { buildChecklist, nextMonth, pendingOwners, progressByUnit } from './fpaHome.ts'

const base = { unallocated: { count: 0, amount: 0n }, progress: { total: 2, completed: 0 }, today: '2026-10-09' }

test('buildChecklist: 最初の未完了が「今ここ」', () => {
  const { steps, next } = buildChecklist({ ...base, lastMonth: '2026-09', unallocated: { count: 3, amount: 120000n }, active: { name: '9月見込', actual_through: '2026-08', update_deadline: null } })
  assert.deepEqual(steps.map((s) => s.state), ['done', 'current', 'todo', 'todo', 'todo'])
  assert.equal(steps[0].label, '9月の実績を取り込む')
  assert.equal(steps[1].detail, '未割当 3 件（120,000 円）')
  assert.equal(next, null)
})

test('buildChecklist: 見込を始めて締切を入れたら、現場の更新が「今ここ」。全部済むと次の月を案内する', () => {
  const active = { name: '10月見込', actual_through: '2026-09', update_deadline: '2026-10-15' }
  let r = buildChecklist({ ...base, lastMonth: '2026-09', active })
  assert.deepEqual(r.steps.map((s) => s.state), ['done', 'done', 'done', 'done', 'current'])
  assert.equal(r.steps[4].detail, '完了 0 / 2 件')
  r = buildChecklist({ ...base, lastMonth: '2026-09', active, progress: { total: 2, completed: 2 } })
  assert.equal(r.next, '次は10月の実績の取込です（会計の締めの後）')
})

test('buildChecklist: 今回の見込がなければ③が「今ここ」', () => {
  const { steps } = buildChecklist({ ...base, lastMonth: '2026-09', active: null })
  assert.equal(steps.find((s) => s.state === 'current')?.key, 'start')
  assert.equal(steps[2].detail, '今回の見込がありません')
})

test('nextMonth', () => {
  assert.equal(nextMonth('2026-12'), '2027-01')
  assert.equal(nextMonth('2026-03'), '2026-04')
})

const it = (unit_id: number, owner_user_id: number | null, status: ActivityProgress['status'], last_edited_at: string | null = null) => ({ unit_id, owner_user_id, status, last_edited_at }) as ActivityProgress

test('progressByUnit と pendingOwners', () => {
  const items = [it(1, 10, 'completed'), it(1, 10, 'in_progress', '2026-10-05T00:00:00Z'), it(2, 11, 'not_started'), it(2, 11, 'not_started'), it(2, null, 'not_started'), it(1, 10, 'not_started', '2026-10-07T00:00:00Z')]
  const units = progressByUnit(items)
  assert.deepEqual(units.map((u) => [u.unitId, u.counts.completed, u.total]), [[2, 0, 3], [1, 1, 3]])
  const owners = pendingOwners(items)
  assert.deepEqual(owners.map((o) => [o.userId, o.notStarted, o.inProgress]), [[10, 1, 1], [11, 2, 0], [null, 1, 0]])
  assert.equal(owners[0].lastEditedAt, '2026-10-07T00:00:00Z')
})
