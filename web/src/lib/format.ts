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

// --- 金額の表示単位（docs/plan.md「7. 前提・決定事項」） ---
// 確認・報告の画面は、画面ごとに 百万円（既定、小数点以下1桁）・千円・円 を切り替える。
// 数値の入力・実績の明細・変更履歴・CSV・報告資料の Excel は円のまま。

export type AmountUnit = 'million' | 'thousand' | 'yen'

export const amountUnitLabels: Record<AmountUnit, string> = { million: '百万円', thousand: '千円', yen: '円' }

/** 円の整数を、単位の最小桁（百万円は 10万円、千円は 1,000円）で四捨五入した値（0 から遠い方へ） */
function roundTo(v: bigint, step: bigint): bigint {
  const q = (v < 0n ? -v : v) * 2n + step
  const r = q / (step * 2n)
  return v < 0n ? -r : r
}

/** 金額（円）を単位で表す。百万円は「158.9」、千円は「158,940」、円は「158,940,000」。マイナスは ▲ */
export function formatAmount(v: bigint | string | number | null | undefined, unit: AmountUnit): string {
  if (v === null || v === undefined || v === '') return ''
  const n = typeof v === 'bigint' ? v : BigInt(typeof v === 'number' ? Math.round(v) : v)
  if (unit === 'yen') return formatYen(String(n))
  // 丸めて 0 になるマイナスも「▲0.0」と表し、マイナスであることを残す
  const sign = n < 0n ? minusSign : ''
  if (unit === 'thousand') {
    const r = roundTo(n, 1000n)
    return `${sign}${yen.format(r < 0n ? -r : r)}`
  }
  const tenth = roundTo(n, 100_000n) // 0.1 百万円の単位
  const abs = tenth < 0n ? -tenth : tenth
  return `${sign}${yen.format(abs / 10n)}.${abs % 10n}`
}

/** 差の金額を単位で表す。プラスは「+3.4」、マイナスは「▲6.2」、0 ちょうどは「0.0」 */
export function formatSignedAmount(v: bigint | string | number | null | undefined, unit: AmountUnit): string {
  const s = formatAmount(v, unit)
  if (s === '' || s.startsWith(minusSign)) return s
  // 0 ちょうどは符号なし。丸めて 0 になるプラスは「+0.0」
  const n = typeof v === 'bigint' ? v : BigInt(typeof v === 'number' ? Math.round(v) : (v as string))
  return n === 0n ? s : `+${s}`
}
