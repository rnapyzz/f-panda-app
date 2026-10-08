import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Milestone, Scenario } from '../api/types.ts'
import { initialScenario, initialTab, lateMilestones } from './activityPage.ts'

test('initialTab: 指定がなければ、編集できて今回の見込があるときだけ「今回の更新」', () => {
  assert.equal(initialTab('settings', false, false), 'settings')
  assert.equal(initialTab(null, true, true), 'update')
  assert.equal(initialTab(null, true, false), 'overview')
  assert.equal(initialTab('x', false, true), 'overview')
})

const sc = (id: number, o: Partial<Scenario> = {}) => ({ id, name: `S${id}`, fiscal_year: 2026, plan_role: null, is_active: false, created_at: `2026-0${id}-01`, ...o }) as Scenario

test('initialScenario: URL → 今回の見込 → 今年度の最新見込', () => {
  const list = [sc(1, { plan_role: 'initial' }), sc(2, { plan_role: 'latest' }), sc(3, { is_active: true })]
  assert.equal(initialScenario('1', list)?.id, 1)
  assert.equal(initialScenario(null, list)?.id, 3)
  assert.equal(initialScenario(null, list.slice(0, 2), new Date('2026-10-09'))?.id, 2)
  assert.equal(initialScenario(null, []), undefined)
})

test('lateMilestones: 期日を過ぎた未完了と、遅延', () => {
  const m = (id: number, due_date: string, status: Milestone['status']) => ({ id, due_date, status }) as Milestone
  const got = lateMilestones([m(1, '2026-10-01', 'in_progress'), m(2, '2026-10-01', 'completed'), m(3, '2026-12-01', 'delayed'), m(4, '2026-12-01', 'not_started')], '2026-10-09')
  assert.deepEqual(got.map((x) => x.id), [1, 3])
})
