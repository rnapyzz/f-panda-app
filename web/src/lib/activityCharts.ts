// 施策の一覧の「図で見る」（ポートフォリオ・マップとツリーマップ）の計算。金額は BigInt で受け取り、描画用に number にする。

import type { RiskActivity } from '../api/types.ts'

/** 施策の一覧の図の種類（docs/plan.md「2.22」） */
export type ChartView = 'portfolio' | 'treemap' | 'range' | 'waterfall' | 'timeline' | 'pipeline'

export const chartViewLabels: Record<ChartView, string> = {
  portfolio: 'ポートフォリオ',
  treemap: 'ツリーマップ',
  range: '振れ幅',
  waterfall: '増減の内訳',
  timeline: 'スケジュール',
  pipeline: '確度の推移',
}

export type Rect = { x: number; y: number; w: number; h: number }

/**
 * 面積が value に比例するように、矩形を並べる（squarified treemap）。value が 0 以下の項目は除く。
 * 返す矩形は items と同じ順番ではなく、値の大きい順。
 */
export function squarify<T extends { value: number }>(items: T[], rect: Rect): (T & { rect: Rect })[] {
  const list = items.filter((it) => it.value > 0).sort((a, b) => b.value - a.value)
  const total = list.reduce((s, it) => s + it.value, 0)
  if (total === 0 || rect.w <= 0 || rect.h <= 0) return []
  const scale = (rect.w * rect.h) / total
  const out: (T & { rect: Rect })[] = []
  let { x, y, w, h } = rect
  let i = 0
  while (i < list.length) {
    const side = Math.min(w, h)
    // 行に足すと、最も細長い矩形の縦横比が悪くなるところで止める
    let row: T[] = [list[i]]
    let rowArea = list[i].value * scale
    const worst = (areas: number[], sum: number) => {
      const max = Math.max(...areas)
      const min = Math.min(...areas)
      return Math.max((side * side * max) / (sum * sum), (sum * sum) / (side * side * min))
    }
    let j = i + 1
    while (j < list.length) {
      const next = list[j].value * scale
      const areas = [...row.map((it) => it.value * scale)]
      if (worst([...areas, next], rowArea + next) > worst(areas, rowArea)) break
      row = [...row, list[j]]
      rowArea += next
      j++
    }
    // 短い辺に沿って、行を置く
    const thickness = rowArea / side
    let offset = 0
    for (const it of row) {
      const len = (it.value * scale) / thickness
      out.push({ ...it, rect: w >= h ? { x, y: y + offset, w: thickness, h: len } : { x: x + offset, y, w: len, h: thickness } })
      offset += len
    }
    if (w >= h) {
      x += thickness
      w -= thickness
    } else {
      y += thickness
      h -= thickness
    }
    i = j
  }
  return out
}

const num = (s: string) => Number(BigInt(s))
export const profit = (p: { revenue: string; expense: string }) => num(p.revenue) - num(p.expense)

export type BubblePoint = {
  activity: RiskActivity
  /** 確度の割合（加重見込の売上 ÷ 満額の売上）。0〜1 */
  certainty: number
  profit: number
  revenue: number
}

/** ポートフォリオ・マップの点。売上のない施策（費用だけの施策）は除く */
export function bubblePoints(items: RiskActivity[]): { points: BubblePoint[]; skipped: number } {
  const points: BubblePoint[] = []
  let skipped = 0
  for (const a of items) {
    const revenue = num(a.full.revenue)
    if (revenue <= 0) {
      skipped++
      continue
    }
    points.push({ activity: a, certainty: Math.min(1, Math.max(0, num(a.weighted.revenue) / revenue)), profit: profit(a.full), revenue })
  }
  // 大きい丸を先に描き、小さい丸が隠れないようにする
  points.sort((p, q) => q.revenue - p.revenue)
  return { points, skipped }
}

/** 軸の目盛り（きりのよい数）。min〜max を覆うように、5本前後で区切る。最初と最後が軸の範囲になる */
export function niceTicks(min: number, max: number, count = 5): number[] {
  if (min === max) return [min - 1, min, min + 1]
  const raw = (max - min) / count
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw
  const out: number[] = []
  // toPrecision で浮動小数点の誤差（0.6000000000000001 など）を丸め、+ 0 で -0 を 0 にする
  for (let k = Math.floor(min / step); k <= Math.ceil(max / step); k++) out.push(Number((k * step).toPrecision(12)) + 0)
  return out
}

/** 目標との差の段階（ツリーマップの色）。-2〜2。目標がなければ null */
export type DiffBucket = -2 | -1 | 0 | 1 | 2

/** 加重見込の利益の、目標（比較）との差の率で段階に分ける。±5% 未満は 0、±20% 以上は ±2。基準が 0 のときは差の符号だけで ±1 */
export function diffBucket(current: number, target: number | null): DiffBucket | null {
  if (target === null) return null
  const diff = current - target
  if (target === 0) return diff === 0 ? 0 : diff > 0 ? 1 : -1
  const rate = diff / Math.abs(target)
  if (Math.abs(rate) < 0.05) return 0
  if (rate >= 0.2) return 2
  if (rate <= -0.2) return -2
  return rate > 0 ? 1 : -1
}

export type RangeRow = { activity: RiskActivity; pessimistic: number; weighted: number; optimistic: number; spread: number }

/** 振れ幅（楽観 − 悲観、利益）の大きい順。limit 件までと、残りの件数 */
export function rangeRows(items: RiskActivity[], limit = 15): { rows: RangeRow[]; rest: number } {
  const rows = items
    .map((a) => {
      const pessimistic = profit(a.pessimistic)
      const optimistic = profit(a.optimistic)
      return { activity: a, pessimistic, weighted: profit(a.weighted), optimistic, spread: optimistic - pessimistic }
    })
    .filter((r) => r.spread !== 0 || r.weighted !== 0)
    .sort((p, q) => q.spread - p.spread || p.activity.code.localeCompare(q.activity.code))
  return { rows: rows.slice(0, limit), rest: Math.max(0, rows.length - limit) }
}

export type WaterfallStep = { key: string; label: string; activityId: number | null; kind: 'total' | 'delta'; from: number; to: number }

/**
 * 比較（目標・前回の見込）から今回への、加重見込の利益の増減。増減の大きい limit 件を施策ごとに、残りは「その他」にまとめる。
 * 比較のない施策は、比較を 0 として数える（新しい施策）。
 */
export function waterfallSteps(items: RiskActivity[], startLabel: string, limit = 8): WaterfallStep[] {
  const deltas = items
    .map((a) => ({ a, delta: profit(a.weighted) - (a.compare ? profit(a.compare) : 0) }))
    .filter((d) => d.delta !== 0)
    .sort((p, q) => Math.abs(q.delta) - Math.abs(p.delta) || p.a.code.localeCompare(q.a.code))
  const start = items.reduce((s, a) => s + (a.compare ? profit(a.compare) : 0), 0)
  const end = items.reduce((s, a) => s + profit(a.weighted), 0)
  const steps: WaterfallStep[] = [{ key: 'start', label: startLabel, activityId: null, kind: 'total', from: 0, to: start }]
  let at = start
  for (const d of deltas.slice(0, limit)) {
    steps.push({ key: `a${d.a.id}`, label: d.a.name, activityId: d.a.id, kind: 'delta', from: at, to: at + d.delta })
    at += d.delta
  }
  const others = deltas.slice(limit)
  if (others.length > 0) {
    const sum = others.reduce((s, d) => s + d.delta, 0)
    steps.push({ key: 'others', label: `その他（${others.length}件）`, activityId: null, kind: 'delta', from: at, to: at + sum })
    at += sum
  }
  steps.push({ key: 'end', label: '今回の見込', activityId: null, kind: 'total', from: 0, to: end })
  return steps
}

/** 月ごとに、売上（満額）を段階（実績・段階のコード・downside）で合計する */
export function pipelineByMonth(items: RiskActivity[], months: string[]): Map<string, Map<string, number>> {
  const out = new Map(months.map((m) => [m, new Map<string, number>()]))
  for (const a of items) {
    for (const [m, parts] of Object.entries(a.revenue_by_month ?? {})) {
      const month = out.get(m)
      if (!month) continue
      for (const [k, v] of Object.entries(parts)) month.set(k, (month.get(k) ?? 0) + num(v))
    }
  }
  return out
}

/** 日付（YYYY-MM-DD）を、年度（4月〜翌3月）の中の位置 0〜1 にする（範囲の外は 0 未満・1 超） */
export function yearPosition(date: string, fiscalYear: number): number {
  const start = Date.UTC(fiscalYear, 3, 1)
  const end = Date.UTC(fiscalYear + 1, 3, 1)
  const [y, m, d] = date.split('-').map(Number)
  return (Date.UTC(y, m - 1, d) - start) / (end - start)
}
