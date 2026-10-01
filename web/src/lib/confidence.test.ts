import assert from 'node:assert/strict'
import { test } from 'node:test'
import { confidenceLabel, ratePercent } from './confidence.ts'

test('ratePercent', () => {
  assert.equal(ratePercent('0.8'), '80%')
  assert.equal(ratePercent(0.125), '12.5%')
  assert.equal(ratePercent(1), '100%')
})

test('confidenceLabel: マスタにあれば名前と確率を付ける', () => {
  const levels = [{ id: 1, code: 'C', name: '中', rate: '0.5', criteria: '', sort_order: 3, created_at: '', updated_at: '' }]
  assert.equal(confidenceLabel('C', levels), 'C 中（50%）')
  assert.equal(confidenceLabel('Z', levels), 'Z')
  assert.equal(confidenceLabel('C', undefined), 'C')
})
