import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { PlanRole } from '../api/types.ts'
import { actualThroughLabel, defaultScenarios, scenarioLabel } from './scenario.ts'

const s = (id: number, plan_role: PlanRole | null = null, is_active = false) => ({ id, name: `S${id}`, plan_role, is_active })

test('scenarioLabel: エイリアスがあれば併記する', () => {
  assert.equal(scenarioLabel(s(1, 'initial')), 'S1（期初計画）')
  assert.equal(scenarioLabel(s(2)), 'S2')
})

test('actualThroughLabel', () => {
  assert.equal(actualThroughLabel('2026-09'), '実績〜9月')
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
