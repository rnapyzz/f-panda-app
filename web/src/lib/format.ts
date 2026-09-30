// 表示用の書式。

const yen = new Intl.NumberFormat('ja-JP')

/** 金額（円）を 3 桁区切りにする。値は文字列または数値（大きな整数も桁落ちさせない） */
export function formatYen(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return ''
  const s = String(v)
  if (/^-?\d+$/.test(s)) {
    return yen.format(BigInt(s))
  }
  return s
}

/** 数値を桁区切りにする（小数を含む） */
export function formatNumber(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return ''
  const s = String(v)
  const [int, frac] = s.split('.')
  const i = /^-?\d+$/.test(int) ? yen.format(BigInt(int)) : int
  return frac ? `${i}.${frac}` : i
}

/** "2026-10" → "10月" */
export function monthLabel(ym: string): string {
  return `${Number(ym.slice(5, 7))}月`
}

/** "2026-10" → "2026年10月" */
export function yearMonthLabel(ym: string): string {
  return `${ym.slice(0, 4)}年${Number(ym.slice(5, 7))}月`
}

/** 確度（0〜1）をパーセント表記にする */
export function formatPercent(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return '—'
  return `${Math.round(Number(v) * 1000) / 10}%`
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString('ja-JP', { dateStyle: 'short', timeStyle: 'short' })
}
