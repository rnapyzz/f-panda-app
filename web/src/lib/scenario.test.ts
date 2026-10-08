import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { PlanRole } from '../api/types.ts'
import { actualThroughLabel, currentFiscalYear, deadlineStatus, defaultScenarios, fiscalMonths, scenarioLabel, todayInTokyo } from './scenario.ts'

const s = (id: number, plan_role: PlanRole | null = null, is_active = false) => ({ id, name: `S${id}`, plan_role, is_active })

test('scenarioLabel: エイリアスがあれば併記する', () => {
  assert.equal(scenarioLabel(s(1, 'initial')), 'S1（期初計画）')
  assert.equal(scenarioLabel(s(2)), 'S2')
})

test('actualThroughLabel', () => {
  assert.equal(actualThroughLabel('2026-09'), '9月まで実績')
  assert.equal(actualThroughLabel(null), '全月計画')
})

test('defaultScenarios: 基準は修正計画 → 期初計画、最新は最新見込', () => {
  assert.deepEqual(defaultScenarios([s(1, 'initial'), s(2, 'revised'), s(3, 'latest'), s(4)]), { base: s(2, 'revised'), latest: s(3, 'latest') })
  assert.deepEqual(defaultScenarios([s(1, 'initial'), s(3, 'latest')]), { base: s(1, 'initial'), latest: s(3, 'latest') })
})

test('defaultScenarios: エイリアスがなければ、作成中・新しいシナリオを使う', () => {
  assert.deepEqual(defaultScenarios([s(1), s(2, null, true), s(3)]), { base: s(1), latest: s(2, null, true) })
  assert.deepEqual(defaultScenarios([s(1), s(5), s(3)]), { base: s(1), latest: s(5) })
  assert.deepEqual(defaultScenarios([s(1)]), { base: s(1), latest: s(1) })
  assert.deepEqual(defaultScenarios([]), { base: undefined, latest: undefined })
})

test('currentFiscalYear・fiscalMonths: 4月始まり', () => {
  assert.equal(currentFiscalYear(new Date(2026, 3, 1)), 2026)
  assert.equal(currentFiscalYear(new Date(2027, 2, 31)), 2026)
  assert.deepEqual([fiscalMonths(2026)[0], fiscalMonths(2026)[9], fiscalMonths(2026)[11]], ['2026-04', '2027-01', '2027-03'])
})

test('deadlineStatus: 残り日数と、3日以内・超過の色分け', () => {
  assert.equal(deadlineStatus(null, '2026-10-07'), null)
  assert.deepEqual(deadlineStatus('2026-10-15', '2026-10-07'), { text: '締切 10/15（あと8日）', tone: 'normal' })
  assert.deepEqual(deadlineStatus('2026-10-15', '2026-10-12'), { text: '締切 10/15（あと3日）', tone: 'soon' })
  assert.deepEqual(deadlineStatus('2026-10-15', '2026-10-15'), { text: '締切 10/15（今日）', tone: 'soon' })
  assert.deepEqual(deadlineStatus('2026-10-15', '2026-10-17'), { text: '締切 10/15（2日過ぎています）', tone: 'overdue' })
})

test('todayInTokyo: 日本時間の日付', () => {
  assert.equal(todayInTokyo(new Date('2026-10-06T15:30:00Z')), '2026-10-07')
  assert.equal(todayInTokyo(new Date('2026-10-06T14:59:00Z')), '2026-10-06')
})
