// 図の色（docs/plan.md「2.22」）。dataviz の検証スクリプトで見分けやすさを確かめた値（白い面の上）。
// 文字は色を付けず、色は印（丸・棒・面）だけに使う。

/** 分類の色（施策のタイプ）。紫・青緑・橙。3色のすべての組で、色覚の違いがあっても見分けられる（ΔE 14 以上） */
export const typeColor = { recurring: '#6e56cf', project: '#12a594', cost_pool: '#f76b15' } as const

/** 段階の色（確度 A → E。同じ紫で濃い → 淡い。いちばん淡い色も面と 2:1 以上） */
export const levelRamp = ['#35258a', '#5340b8', '#7562d9', '#9686e6', '#b2a6ef']
/** 実績・ダウンサイド */
export const actualColor = '#64748b'
export const downsideColor = '#e5484d'

/** 増減（ウォーターフォール）と、目標との差の発散の色。青緑 = 上振れ、灰 = ほぼ同じ、赤 = 下振れ */
export const upColor = '#12a594'
export const downColor = '#e5484d'
export const totalColor = '#475569'
export const diverging = { strongDown: '#e5484d', down: '#f5adb0', mid: '#e7e7ec', up: '#98dccf', strongUp: '#12a594', none: '#f3f3f6' } as const

/** 図の地・線・文字 */
export const ink = {
  primary: '#0f172a',
  secondary: '#475569',
  muted: '#94a3b8',
  grid: '#eef0f4',
  axis: '#cbd2dc',
  surface: '#ffffff',
  accent: '#6e56cf',
  accentSoft: '#e4dffb',
} as const
