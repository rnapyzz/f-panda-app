import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { ReportRow, TreeNode, Unit } from '../api/types.ts'
import { breadcrumb, causeKeyOf, childGroups, totalsByActivity, filtersQuery, groupIncludes, parseFilters, scopeOf, scopedRows, totalsBySeries, varianceBars, waterfall, type DashActivity, type DashFilters } from './dashboard.ts'
import { buildTree } from './tree.ts'

// セグメント: SaaS(1) / 受託(2) → 受託の下に 国内(3)。ユニット: SaaS U(10, seg 1), 受託 U(20, seg 3), 共通 U(30, seg 1, 共通費)
const seg = (id: number, parent: number | null, name: string) => ({ id, parent_id: parent, code: `S${id}`, name, level: 0, sort_order: id }) as TreeNode
const tree = buildTree([seg(1, null, 'SaaS'), seg(2, null, '受託'), seg(3, 2, '国内')])
const unit = (id: number, segment: number, owner: number | null, type: Unit['unit_type'] = 'service') =>
  ({ id, code: `U${id}`, name: `U${id}`, unit_type: type, segment_id: segment, organization_id: 1, owner_user_id: owner, is_archived: false }) as Unit
const units = [unit(10, 1, 7), unit(20, 3, 8), unit(30, 1, 7, 'cost_center')]
const act = (id: number, unit_id: number, owner: number | null, extra: Partial<DashActivity> = {}): DashActivity => ({
  id, code: `A${id}`, name: `施策${id}`, unit_id, owner_user_id: owner, activity_type: 'recurring', confidence_level: 'A', causes: [], ...extra,
})
const activities = [act(100, 10, 5), act(101, 10, 6, { activity_type: 'project', confidence_level: 'C', causes: ['timing'] }), act(200, 20, 5, { causes: ['new', 'lost'] }), act(300, 30, null)]
const base: DashFilters = { fy: 2026, scenario: 1, compare: 'target', scope: 'all', type: '', unitType: '', segment: null, unit: null, cause: null, level: null, measure: 'profit' }

test('parseFilters / filtersQuery: 既定は書かず、往復できる', () => {
  const f = parseFilters(new URLSearchParams('fy=2026&scenario=3&segment=2&cause=new&measure=revenue&scope=bogus'), { scope: 'mine' })
  assert.equal(f.scope, 'mine')
  assert.equal(f.segment, 2)
  assert.equal(f.cause, 'new')
  assert.equal(filtersQuery(f, { scope: 'mine' }), '?fy=2026&scenario=3&segment=2&cause=new&measure=revenue')
  assert.equal(filtersQuery(parseFilters(new URLSearchParams(''), { scope: 'all' }), { scope: 'all' }), '')
})

test('causeKeyOf: 分類なし・1つ・複数', () => {
  assert.equal(causeKeyOf({ causes: [] }), 'none')
  assert.equal(causeKeyOf({ causes: ['timing'] }), 'timing')
  assert.equal(causeKeyOf({ causes: ['new', 'lost'] }), 'multiple')
})

test('scopeOf: 掘り下げ・範囲・絞り込み', () => {
  const all = scopeOf(base, tree, units, activities, { id: 7 })
  assert.deepEqual([...all.units].sort(), [10, 20, 30])
  assert.equal(all.filtered, false)
  assert.equal(all.withUnallocated, true)

  const saas = scopeOf({ ...base, segment: 1 }, tree, units, activities, { id: 7 })
  assert.deepEqual([...saas.units].sort(), [10, 30])
  assert.equal(saas.withUnallocated, false)

  // 受託（2）の下の国内（3）のユニットも含む
  assert.deepEqual([...scopeOf({ ...base, segment: 2 }, tree, units, activities, { id: 7 }).units], [20])
  // 所管ユニット（担当 7）
  assert.deepEqual([...scopeOf({ ...base, scope: 'units' }, tree, units, activities, { id: 7 }).units].sort(), [10, 30])
  // 自分が担当の施策（5）: 施策のあるユニットだけ、施策ごとの行で集計
  const mine = scopeOf({ ...base, scope: 'mine' }, tree, units, activities, { id: 5 })
  assert.deepEqual([...mine.activities].sort(), [100, 200])
  assert.deepEqual([...mine.units].sort(), [10, 20])
  assert.equal(mine.filtered, true)
  // タイプ・要因・確度・ユニットの種別
  assert.deepEqual([...scopeOf({ ...base, type: 'project' }, tree, units, activities, { id: 1 }).activities], [101])
  assert.deepEqual([...scopeOf({ ...base, cause: 'multiple' }, tree, units, activities, { id: 1 }).activities], [200])
  assert.deepEqual([...scopeOf({ ...base, level: 'C' }, tree, units, activities, { id: 1 }).activities], [101])
  assert.deepEqual([...scopeOf({ ...base, unitType: 'cost_center' }, tree, units, activities, { id: 1 }).units], [30])
})

test('childGroups: 全社 → セグメント、末端のセグメント → ユニット、ユニット → 施策', () => {
  const names = (f: DashFilters) => childGroups(f, tree, units, activities, scopeOf(f, tree, units, activities, { id: 1 })).map((g) => g.key)
  assert.deepEqual(names(base), ['s1', 's2', 'unallocated'])
  assert.deepEqual(names({ ...base, segment: 2 }), ['s3'])
  assert.deepEqual(names({ ...base, segment: 1 }), ['u10', 'u30'])
  assert.deepEqual(names({ ...base, segment: 1, unit: 10 }), ['a100', 'a101'])
  // 対象のユニットがないセグメントは出さない
  assert.deepEqual(names({ ...base, unitType: 'cost_center' }), ['s1'])
})

const row = (unit_id: number | null, subject_id: number, month: string, values: Record<string, string>, activity_id?: number): ReportRow => ({ unit_id, subject_id, month, values, activity_id })
const categoryOf = (id: number) => (id === 1 ? 'revenue' : id === 2 ? 'expense' : undefined)
const months = ['2026-04', '2026-05']
const unitRows = [
  row(10, 1, '2026-04', { c: '100', t: '80' }),
  row(10, 2, '2026-04', { c: '30', t: '20' }),
  row(20, 1, '2026-05', { c: '50', t: '90' }),
  row(null, 1, '2026-04', { c: '7', t: '7' }),
]

// 施策ごとの行（ユニットを指定して取得したもの）
const activityRows = [
  row(10, 1, '2026-04', { c: '60', t: '50' }, 100),
  row(10, 1, '2026-04', { c: '40', t: '30' }, 101),
  row(10, 2, '2026-04', { c: '30', t: '20' }, 100),
  row(20, 1, '2026-05', { c: '50', t: '90' }, 200),
]

test('scopedRows・totalsBySeries: 施策ごとの行で集計し、未割当は全社のときだけ', () => {
  const all = scopeOf(base, tree, units, activities, { id: 1 })
  const t = totalsBySeries(scopedRows(unitRows, activityRows, all), ['c', 't'], categoryOf, months)
  assert.deepEqual(t.get('c')!.year, { revenue: 157n, expense: 30n })
  assert.deepEqual(t.get('c')!.byMonth.get('2026-05'), { revenue: 50n, expense: 0n })
  // 同じ系列を2回渡しても、2回は数えない（比較と前回の見込が同じシナリオのとき）
  assert.deepEqual(totalsBySeries(scopedRows(unitRows, activityRows, all), ['c', 't', 't'], categoryOf, months).get('t')!.year, { revenue: 177n, expense: 20n })
  const saas = scopeOf({ ...base, segment: 1 }, tree, units, activities, { id: 1 })
  assert.deepEqual(totalsBySeries(scopedRows(unitRows, activityRows, saas), ['c'], categoryOf, months).get('c')!.year, { revenue: 100n, expense: 30n })
  const project = scopeOf({ ...base, type: 'project' }, tree, units, activities, { id: 1 })
  assert.deepEqual(totalsBySeries(scopedRows(unitRows, activityRows, project), ['c'], categoryOf, months).get('c')!.year, { revenue: 40n, expense: 0n })
})

test('varianceBars: 区分ごとの差（利益）を、上振れから順に', () => {
  const f = base
  const scope = scopeOf(f, tree, units, activities, { id: 1 })
  const groups = childGroups(f, tree, units, activities, scope)
  const bars = varianceBars(groups, scopedRows(unitRows, activityRows, scope), (g) => groupIncludes(g, tree, units), { current: 'c', compare: 't' }, categoryOf, months)
  assert.deepEqual(
    bars.map((b) => [b.group.key, b.current, b.compare, b.diff]),
    [
      ['s1', 70n, 60n, 10n],
      ['unallocated', 7n, 7n, 0n],
      ['s2', 50n, 90n, -40n],
    ],
  )
})

test('waterfall: 前回 → 区分ごとの増減（大きい順、残りはその他）→ 今回', () => {
  const e = (id: number, current: number, previous: number) => ({ key: `k${id}`, label: `L${id}`, filter: null, current: BigInt(current), previous: BigInt(previous) })
  const steps = waterfall([e(1, 100, 80), e(2, 50, 90), e(3, 10, 5), e(4, 30, 0), e(5, 20, 20)], 2)
  assert.deepEqual(
    steps.map((s) => [s.label, s.from, s.to]),
    [
      ['前回の見込', 0n, 195n],
      ['L2', 195n, 155n],
      ['L4', 155n, 185n],
      ['その他（2件）', 185n, 210n],
      ['今回の見込', 0n, 210n],
    ],
  )
})

test('breadcrumb: 全社 › セグメントの階層 › ユニット', () => {
  assert.deepEqual(
    breadcrumb({ ...base, segment: 3, unit: 20 }, tree, units).map((b) => b.label),
    ['全社', '受託', '国内', 'U20'],
  )
})

test('totalsByActivity: 施策ごと（未割当は 0）の年間の金額', () => {
  const t = totalsByActivity([...activityRows, row(null, 1, '2026-04', { c: '7', t: '7' }), row(10, 1, '2027-04', { c: '999' }, 100)], ['c', 't'], categoryOf, months)
  assert.deepEqual(t.get(100)!.get('c'), { revenue: 60n, expense: 30n })
  assert.deepEqual(t.get(200)!.get('t'), { revenue: 90n, expense: 0n })
  assert.deepEqual(t.get(0)!.get('c'), { revenue: 7n, expense: 0n })
  assert.deepEqual(totalsByActivity(activityRows, ['t', 't'], categoryOf, months).get(100)!.get('t'), { revenue: 50n, expense: 20n })
})
