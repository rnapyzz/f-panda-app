// ダッシュボード（docs/plan.md「2.30」）の絞り込み・掘り下げ・集計。金額は BigInt で扱う。
//
// 金額の出どころ:
//   - 金額（年間の状況・月次の推移・どこで差が出ているか・前回から何が変わったか・施策の表）: 予実比較 API の施策 × 科目 × 月の行
//     （対象のユニットごとに unit_id を付けて取得）と、未割当の実績の行
//   - 施策のタイプ・担当者・確度の段階: 施策の一覧。要因の分類・更新の状態: activity-status
//   - どれくらい確かか: リスク API の施策ごとの楽観・加重見込・悲観と確度の構成

import type { ActivityType, NoteCause, ReportRow, SubjectCategory, TreeNode, Unit, UnitType } from '../api/types'
import { aggregate, type Measure } from './aggregate.ts'
import { subtreeIds, type Tree } from './tree.ts'

/** 前回の見込からの増減の分け方（要因の分類）。分類なし・複数の分類も1つの区分にする */
export type CauseKey = NoteCause | 'none' | 'multiple'

export type DashScope = 'all' | 'units' | 'mine'

export const scopeLabels: Record<DashScope, string> = { all: '全社', units: '所管ユニット', mine: '自分が担当の施策' }

/** 絞り込みと掘り下げ（URL のクエリに持つ） */
export type DashFilters = {
  fy: number | null
  scenario: number | null
  compare: 'target' | 'previous'
  scope: DashScope
  type: ActivityType | ''
  unitType: UnitType | ''
  /** 掘り下げの位置（セグメント、ユニット） */
  segment: number | null
  unit: number | null
  /** 図から選んだ絞り込み（要因の分類、確度の段階） */
  cause: CauseKey | null
  level: string | null
  /** 月次の推移で見る金額 */
  measure: Measure
}

const causes: CauseKey[] = ['timing', 'volume', 'new', 'lost', 'assumption', 'other', 'none', 'multiple']

const num = (v: string | null) => (v && /^\d+$/.test(v) ? Number(v) : null)

/** URL のクエリから絞り込みを読む。ないものは defaults */
export function parseFilters(search: URLSearchParams, defaults: Pick<DashFilters, 'scope'>): DashFilters {
  const pick = <T extends string>(key: string, allowed: readonly T[], fallback: T): T => {
    const v = search.get(key) as T | null
    return v && allowed.includes(v) ? v : fallback
  }
  return {
    fy: num(search.get('fy')),
    scenario: num(search.get('scenario')),
    compare: pick('compare', ['target', 'previous'] as const, 'target'),
    scope: pick('scope', ['all', 'units', 'mine'] as const, defaults.scope),
    type: pick('type', ['', 'project', 'recurring', 'cost_pool'] as const, ''),
    unitType: pick('unit_type', ['', 'service', 'cost_center', 'corporate'] as const, ''),
    segment: num(search.get('segment')),
    unit: num(search.get('unit')),
    cause: search.get('cause') && causes.includes(search.get('cause') as CauseKey) ? (search.get('cause') as CauseKey) : null,
    level: search.get('level') || null,
    measure: pick('measure', ['profit', 'revenue', 'expense'] as const, 'profit'),
  }
}

/** 絞り込みを URL のクエリにする（既定の値は書かない） */
export function filtersQuery(f: DashFilters, defaults: Pick<DashFilters, 'scope'>): string {
  const q = new URLSearchParams()
  if (f.fy !== null) q.set('fy', String(f.fy))
  if (f.scenario !== null) q.set('scenario', String(f.scenario))
  if (f.compare !== 'target') q.set('compare', f.compare)
  if (f.scope !== defaults.scope) q.set('scope', f.scope)
  if (f.type) q.set('type', f.type)
  if (f.unitType) q.set('unit_type', f.unitType)
  if (f.segment !== null) q.set('segment', String(f.segment))
  if (f.unit !== null) q.set('unit', String(f.unit))
  if (f.cause) q.set('cause', f.cause)
  if (f.level) q.set('level', f.level)
  if (f.measure !== 'profit') q.set('measure', f.measure)
  const s = q.toString()
  return s ? `?${s}` : ''
}

/** 施策の情報（activity-status とリスク API を合わせたもの） */
export type DashActivity = {
  id: number
  code: string
  name: string
  unit_id: number
  owner_user_id: number | null
  activity_type: ActivityType | null
  confidence_level: string | null
  causes: NoteCause[]
}

export function causeKeyOf(a: Pick<DashActivity, 'causes'>): CauseKey {
  if (a.causes.length === 0) return 'none'
  if (a.causes.length > 1) return 'multiple'
  return a.causes[0]
}

/** 対象の範囲。units は表示するユニット、activities は表示する施策、filtered は施策の条件で絞っているか、withUnallocated は未割当の実績を含めるか */
export type Scope = { units: Set<number>; activities: Set<number>; filtered: boolean; withUnallocated: boolean }

/** 絞り込み・掘り下げの位置から、対象のユニットと施策を決める */
export function scopeOf(f: DashFilters, tree: Tree, units: Unit[], activities: DashActivity[], me: { id: number }): Scope {
  const inSegment = f.segment !== null ? subtreeIds(tree, f.segment) : null
  const unitIds = new Set(
    units
      .filter((u) => !u.is_archived || activities.some((a) => a.unit_id === u.id))
      .filter((u) => !f.unitType || u.unit_type === f.unitType)
      .filter((u) => !inSegment || inSegment.has(u.segment_id))
      .filter((u) => f.unit === null || u.id === f.unit)
      .filter((u) => f.scope !== 'units' || u.owner_user_id === me.id)
      .map((u) => u.id),
  )
  const acts = activities.filter(
    (a) =>
      unitIds.has(a.unit_id) &&
      (f.scope !== 'mine' || a.owner_user_id === me.id) &&
      (!f.type || a.activity_type === f.type) &&
      (!f.cause || causeKeyOf(a) === f.cause) &&
      (!f.level || a.confidence_level === f.level),
  )
  const filtered = f.scope === 'mine' || !!f.type || !!f.cause || !!f.level
  // 担当の施策で絞るときは、施策のあるユニットだけ
  if (f.scope === 'mine') for (const id of [...unitIds]) if (!acts.some((a) => a.unit_id === id)) unitIds.delete(id)
  // 未割当の実績（docs/plan.md「2.12」）は、全社をそのまま見ているときだけ含める（全社の合計を会計と一致させるため）
  const withUnallocated = f.scope === 'all' && f.segment === null && f.unit === null && !f.unitType && !filtered
  return { units: unitIds, activities: new Set(acts.map((a) => a.id)), filtered, withUnallocated }
}

/**
 * 予実比較の行のうち、対象の行。金額は施策ごとの行（ユニットを指定して取得したもの）で集計し、
 * 全社をそのまま見ているときは未割当の実績（ユニットを指定しない取得の unit_id が null の行）も含める
 */
export function scopedRows(unitRows: ReportRow[], activityRows: ReportRow[], scope: Scope): ReportRow[] {
  const acts = activityRows.filter((r) => r.activity_id !== undefined && scope.activities.has(r.activity_id))
  return scope.withUnallocated ? [...acts, ...unitRows.filter((r) => r.unit_id === null)] : acts
}

/** 図の1つの区分（掘り下げの1段下） */
export type Group = { key: string; kind: 'segment' | 'unit' | 'activity' | 'unallocated'; id: number; name: string }

/** 掘り下げの位置の1段下の区分。ユニットまで来たら施策、セグメントの下にセグメントがなければユニット */
export function childGroups(f: DashFilters, tree: Tree, units: Unit[], activities: DashActivity[], scope: Scope): Group[] {
  if (f.unit !== null) {
    return activities.filter((a) => scope.activities.has(a.id)).map((a) => ({ key: `a${a.id}`, kind: 'activity', id: a.id, name: a.name }))
  }
  const children = tree.children.get(f.segment) ?? []
  const out: Group[] = []
  if (children.length > 0) {
    // 下のセグメントごと（対象のユニットがあるものだけ）
    for (const s of children) {
      const ids = subtreeIds(tree, s.id)
      if (units.some((u) => scope.units.has(u.id) && ids.has(u.segment_id))) out.push({ key: `s${s.id}`, kind: 'segment', id: s.id, name: s.name })
    }
  } else {
    // 末端のセグメント（またはセグメントがない）なら、ユニットごと。ユニットは末端のセグメントに所属する
    for (const u of units) {
      if (scope.units.has(u.id) && (f.segment === null || u.segment_id === f.segment)) out.push({ key: `u${u.id}`, kind: 'unit', id: u.id, name: u.name })
    }
  }
  if (scope.withUnallocated) out.push({ key: 'unallocated', kind: 'unallocated', id: 0, name: '未割当' })
  return out
}

/** 区分に含まれる行か */
export function groupIncludes(g: Group, tree: Tree, units: Unit[]): (row: ReportRow) => boolean {
  if (g.kind === 'activity') return (row) => row.activity_id === g.id
  if (g.kind === 'unit') return (row) => row.unit_id === g.id
  if (g.kind === 'unallocated') return (row) => row.unit_id === null
  const ids = subtreeIds(tree, g.id)
  const unitIds = new Set(units.filter((u) => ids.has(u.segment_id)).map((u) => u.id))
  return (row) => row.unit_id !== null && unitIds.has(row.unit_id)
}

export type PL = { revenue: bigint; expense: bigint }
export const profitOf = (p: PL) => p.revenue - p.expense
export const measureValue = (p: PL | undefined, m: Measure) => (!p ? 0n : m === 'revenue' ? p.revenue : m === 'expense' ? p.expense : p.revenue - p.expense)

/** 系列ごとの年間の金額と月別の金額 */
export function totalsBySeries(
  rows: ReportRow[],
  keys: string[],
  categoryOf: (subjectId: number) => SubjectCategory | undefined,
  months: string[],
  include: (row: ReportRow) => boolean = () => true,
): Map<string, { year: PL; byMonth: Map<string, PL> }> {
  // 比較と前回の見込が同じシナリオのときなど、同じ系列は1回だけ数える
  const unique = [...new Set(keys)]
  const agg = aggregate(rows, unique, categoryOf, include, new Set(months))
  return new Map(
    unique.map((k) => {
      const t = agg.get(k)!
      return [k, { year: { revenue: t.revenue, expense: t.expense }, byMonth: t.byMonth }]
    }),
  )
}

/** 施策ごと（未割当は 0）の、系列ごとの年間の金額 */
export function totalsByActivity(
  rows: ReportRow[],
  keys: string[],
  categoryOf: (subjectId: number) => SubjectCategory | undefined,
  months: string[],
): Map<number, Map<string, PL>> {
  const inYear = new Set(months)
  keys = [...new Set(keys)]
  const out = new Map<number, Map<string, PL>>()
  for (const r of rows) {
    const category = categoryOf(r.subject_id)
    if (!category || !inYear.has(r.month)) continue
    const id = r.unit_id === null ? 0 : (r.activity_id ?? 0)
    const bySeries = out.get(id) ?? new Map(keys.map((k) => [k, { revenue: 0n, expense: 0n }]))
    for (const k of keys) {
      const raw = r.values[k]
      if (raw !== undefined) bySeries.get(k)![category] += BigInt(raw)
    }
    out.set(id, bySeries)
  }
  return out
}

/** どこで差が出ているか: 区分ごとの今回・比較と差（利益）。差の大きい順（上振れ → 下振れ） */
export type VarianceBar = { group: Group; current: bigint; compare: bigint; diff: bigint }

export function varianceBars(
  groups: Group[],
  rows: ReportRow[],
  includeOf: (g: Group) => (row: ReportRow) => boolean,
  keys: { current: string; compare: string },
  categoryOf: (subjectId: number) => SubjectCategory | undefined,
  months: string[],
): VarianceBar[] {
  return groups
    .map((g) => {
      const t = totalsBySeries(rows, [keys.current, keys.compare], categoryOf, months, includeOf(g))
      const current = profitOf(t.get(keys.current)!.year)
      const compare = profitOf(t.get(keys.compare)!.year)
      return { group: g, current, compare, diff: current - compare }
    })
    .filter((b) => b.current !== 0n || b.compare !== 0n)
    .sort((a, b) => (a.diff === b.diff ? a.group.name.localeCompare(b.group.name, 'ja') : a.diff > b.diff ? -1 : 1))
}

/** 前回から何が変わったか（滝の図）の1段 */
export type Step = { key: string; label: string; kind: 'total' | 'delta'; from: bigint; to: bigint; filter: { kind: 'group'; group: Group } | { kind: 'cause'; cause: CauseKey } | null }

export const causeLabels: Record<CauseKey, string> = {
  timing: '時期のずれ',
  volume: '数量・単価の増減',
  new: '新規',
  lost: '失注・解約',
  assumption: '前提の変化',
  other: 'その他の要因',
  none: '分類なし',
  multiple: '複数の要因',
}

/** 滝の図の1つの区分の前回・今回の利益 */
export type WaterfallEntry = { key: string; label: string; filter: Step['filter']; previous: bigint; current: bigint }

/**
 * 前回の見込の利益から、区分ごとの増減を積み上げて今回の見込の利益に至る段。
 * 同じ key の entry はまとめる。増減の大きい順に limit 個まで並べ、残りは「その他」にまとめる。増減が 0 の区分は出さない。
 */
export function waterfall(entries: WaterfallEntry[], limit = 8, labels = { start: '前回の見込', end: '今回の見込' }): Step[] {
  let start = 0n
  let end = 0n
  const buckets = new Map<string, { label: string; filter: Step['filter']; delta: bigint }>()
  for (const it of entries) {
    start += it.previous
    end += it.current
    const e = buckets.get(it.key) ?? { label: it.label, filter: it.filter, delta: 0n }
    e.delta += it.current - it.previous
    buckets.set(it.key, e)
  }
  const abs = (v: bigint) => (v < 0n ? -v : v)
  const sorted = [...buckets.entries()].filter(([, b]) => b.delta !== 0n).sort((a, b) => (abs(a[1].delta) === abs(b[1].delta) ? 0 : abs(a[1].delta) > abs(b[1].delta) ? -1 : 1))
  const shown = sorted.slice(0, limit)
  const rest = sorted.slice(limit).reduce((s, [, b]) => s + b.delta, 0n)
  const steps: Step[] = [{ key: 'start', label: labels.start, kind: 'total', from: 0n, to: start, filter: null }]
  let acc = start
  for (const [key, b] of shown) {
    steps.push({ key, label: b.label, kind: 'delta', from: acc, to: acc + b.delta, filter: b.filter })
    acc += b.delta
  }
  if (rest !== 0n) {
    steps.push({ key: 'rest', label: `その他（${sorted.length - shown.length}件）`, kind: 'delta', from: acc, to: acc + rest, filter: null })
    acc += rest
  }
  steps.push({ key: 'end', label: labels.end, kind: 'total', from: 0n, to: end, filter: null })
  return steps
}

/** パンくず（全社 › セグメント… › ユニット） */
export function breadcrumb(f: DashFilters, tree: Tree, units: Unit[]): { label: string; segment: number | null; unit: number | null }[] {
  const out: { label: string; segment: number | null; unit: number | null }[] = [{ label: '全社', segment: null, unit: null }]
  const path: TreeNode[] = []
  let cur = f.segment !== null ? tree.byId.get(f.segment) : undefined
  while (cur) {
    path.unshift(cur)
    cur = cur.parent_id !== null ? tree.byId.get(cur.parent_id) : undefined
  }
  for (const s of path) out.push({ label: s.name, segment: s.id, unit: null })
  if (f.unit !== null) {
    const u = units.find((x) => x.id === f.unit)
    if (u) out.push({ label: u.name, segment: f.segment, unit: u.id })
  }
  return out
}
