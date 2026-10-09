import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { RiskActivity } from '../api/types.ts'
import { bubblePoints, diffBucket, niceTicks, squarify } from './activityCharts.ts'

test('squarify: 面積は値に比例し、矩形は領域を埋める', () => {
  const items = [6, 6, 4, 3, 2, 2, 1].map((value, i) => ({ id: i, value }))
  const rects = squarify(items, { x: 0, y: 0, w: 6, h: 4 })
  assert.equal(rects.length, 7)
  const area = rects.reduce((s, r) => s + r.rect.w * r.rect.h, 0)
  assert.ok(Math.abs(area - 24) < 1e-9)
  for (const r of rects) {
    assert.ok(Math.abs(r.rect.w * r.rect.h - r.value) < 1e-9, `id ${r.id}`)
    assert.ok(r.rect.x >= -1e-9 && r.rect.y >= -1e-9 && r.rect.x + r.rect.w <= 6 + 1e-9 && r.rect.y + r.rect.h <= 4 + 1e-9)
  }
  assert.deepEqual(squarify([{ value: 0 }], { x: 0, y: 0, w: 1, h: 1 }), [])
})

const pl = (revenue: number, expense = 0) => ({ revenue: String(revenue), expense: String(expense) })
const act = (id: number, full: number, weighted: number, expense = 0) => ({ id, full: pl(full, expense), weighted: pl(weighted) }) as RiskActivity

test('bubblePoints: 確度の割合・利益。売上のない施策は除く', () => {
  const { points, skipped } = bubblePoints([act(1, 1000, 500, 200), act(2, 0, 0, 300), act(3, 4000, 4000)])
  assert.equal(skipped, 1)
  assert.deepEqual(
    points.map((p) => [p.activity.id, p.certainty, p.profit]),
    [
      [3, 1, 4000],
      [1, 0.5, 800],
    ],
  )
})

test('niceTicks と diffBucket', () => {
  assert.deepEqual(niceTicks(-1_000_000, 4_200_000), [-2_000_000, 0, 2_000_000, 4_000_000, 6_000_000])
  assert.deepEqual(niceTicks(0, 1), [0, 0.2, 0.4, 0.6, 0.8, 1])
  assert.equal(diffBucket(100, null), null)
  assert.equal(diffBucket(102, 100), 0)
  assert.equal(diffBucket(110, 100), 1)
  assert.equal(diffBucket(70, 100), -2)
  assert.equal(diffBucket(-50, 0), -1)
})
