// 表示用の書式。

const yen = new Intl.NumberFormat('ja-JP')

/** マイナスの記号。画面・図・Excel の金額は「▲1,000」と表す（CSV は「-」のまま） */
export const minusSign = '▲'

/** 整数を 3 桁区切りにし、マイナスは ▲ を付ける（大きな整数も桁落ちさせない） */
function groupInt(int: string): string {
  const negative = int.startsWith('-')
  const body = yen.format(BigInt(negative ? int.slice(1) : int))
  return negative && body !== '0' ? `${minusSign}${body}` : body
}

/** 金額（円）を 3 桁区切りにする。マイナスは「▲1,000」。値は文字列または数値 */
export function formatYen(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return ''
  const s = String(v)
  if (/^-?\d+$/.test(s)) return groupInt(s)
  return s
}

/** 数値を桁区切りにする（小数を含む）。マイナスは ▲ */
export function formatNumber(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return ''
  const s = String(v)
  const [int, frac] = s.split('.')
  if (!/^-?\d+$/.test(int)) return s
  const negative = int.startsWith('-')
  const i = groupInt(int)
  // -0.5 のように整数部が 0 のときも ▲ を付ける
  const head = negative && !i.startsWith(minusSign) && frac ? `${minusSign}${i}` : i
  return frac ? `${head}.${frac}` : head
}

/** 差の金額。プラスは「+1,000」、マイナスは「▲1,000」、0 は「0」 */
export function formatSignedYen(v: bigint | string | number): string {
  const n = BigInt(v)
  return `${n > 0n ? '+' : ''}${formatYen(String(n))}`
}

/** 差の率（%）。プラスは「+5.2%」、マイナスは「▲5.2%」 */
export function formatRate(rate: number): string {
  if (rate < 0) return `${minusSign}${-rate}%`
  return `${rate > 0 ? '+' : ''}${rate}%`
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
