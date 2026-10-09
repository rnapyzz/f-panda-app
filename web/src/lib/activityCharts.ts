// 施策の一覧の「図で見る」（ポートフォリオ・マップとツリーマップ）の計算。金額は BigInt で受け取り、描画用に number にする。

import type { RiskActivity } from '../api/types.ts'

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
