// スプレッドシート風の入力グリッドの、画面に依存しない処理（範囲・移動・クリップボード）。

export type Pos = { r: number; c: number }
export type Range = { top: number; left: number; bottom: number; right: number }

/** 2つのセルを対角とする範囲 */
export function rangeOf(a: Pos, b: Pos): Range {
  return { top: Math.min(a.r, b.r), left: Math.min(a.c, b.c), bottom: Math.max(a.r, b.r), right: Math.max(a.c, b.c) }
}

export function inRange(range: Range, r: number, c: number): boolean {
  return r >= range.top && r <= range.bottom && c >= range.left && c <= range.right
}

/** グリッドの中で (dr, dc) だけ動かす。toEdge なら端まで動かす（Ctrl/⌘ + 矢印） */
export function move(p: Pos, dr: number, dc: number, rows: number, cols: number, toEdge = false): Pos {
  const clamp = (v: number, max: number) => Math.max(0, Math.min(max - 1, v))
  if (toEdge) {
    return { r: dr === 0 ? p.r : dr < 0 ? 0 : rows - 1, c: dc === 0 ? p.c : dc < 0 ? 0 : cols - 1 }
  }
  return { r: clamp(p.r + dr, rows), c: clamp(p.c + dc, cols) }
}

const fullWidth = /[０-９．，－＋]/g
const halfOf: Record<string, string> = { '．': '.', '，': ',', '－': '-', '＋': '+' }

/**
 * 貼り付け・入力された数値の表記をそろえる。
 * 桁区切り・円記号・全角数字、会計表記のマイナス（△1,000 / ▲1,000 / (1,000)）に対応する。
 * 数値として読めない場合は、前後の空白を除いてそのまま返す（保存時にサーバーが検証する）。
 */
export function cleanNumber(raw: string): string {
  let s = raw.trim().replace(fullWidth, (ch) => halfOf[ch] ?? String.fromCharCode(ch.charCodeAt(0) - 0xfee0))
  let negative = false
  if (/^[△▲]/.test(s)) {
    negative = true
    s = s.slice(1)
  } else if (/^\(.*\)$/.test(s)) {
    negative = true
    s = s.slice(1, -1)
  }
  const n = s.replace(/[,\s¥￥円]/g, '')
  if (!/^[-+]?(\d+\.?\d*|\.\d+)$/.test(n)) return raw.trim()
  const v = n.replace(/^\+/, '')
  if (!negative) return v
  return v.startsWith('-') ? v.slice(1) : `-${v}`
}

/**
 * クリップボードのテキスト（Excel・Google スプレッドシートのタブ区切り）を行列にする。
 * ダブルクォートで囲まれたセル（改行・タブを含む）に対応し、末尾の改行は無視する。
 */
export function parseClipboard(text: string): string[][] {
  const rows: string[][] = []
  let row: string[] = []
  let cell = ''
  let quoted = false
  const s = text.replace(/\r\n?/g, '\n')
  for (let i = 0; i < s.length; i++) {
    const ch = s[i]
    if (quoted) {
      if (ch === '"' && s[i + 1] === '"') {
        cell += '"'
        i++
      } else if (ch === '"') {
        quoted = false
      } else {
        cell += ch
      }
    } else if (ch === '"' && cell === '') {
      quoted = true
    } else if (ch === '\t') {
      row.push(cell)
      cell = ''
    } else if (ch === '\n') {
      row.push(cell)
      rows.push(row)
      row = []
      cell = ''
    } else {
      cell += ch
    }
  }
  if (cell !== '' || row.length > 0) {
    row.push(cell)
    rows.push(row)
  }
  return rows
}

/** 行列をタブ区切りのテキストにする（Excel・Google スプレッドシートに貼り付けられる形式） */
export function toTsv(matrix: string[][]): string {
  return matrix.map((row) => row.map((v) => (/[\t\n"]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)).join('\t')).join('\n')
}

/**
 * 貼り付け先のセルと値を求める。
 * 1つの値を範囲に貼り付けた場合は範囲全体を埋める。それ以外は範囲の左上から貼り付け、グリッドの外ははみ出さない。
 */
export function pasteCells(selection: Range, matrix: string[][], rows: number, cols: number): { r: number; c: number; value: string }[] {
  const out: { r: number; c: number; value: string }[] = []
  if (matrix.length === 0) return out
  if (matrix.length === 1 && matrix[0].length === 1) {
    for (let r = selection.top; r <= selection.bottom; r++) {
      for (let c = selection.left; c <= selection.right; c++) out.push({ r, c, value: matrix[0][0] })
    }
    return out
  }
  matrix.forEach((row, i) =>
    row.forEach((value, j) => {
      const r = selection.top + i
      const c = selection.left + j
      if (r < rows && c < cols) out.push({ r, c, value })
    }),
  )
  return out
}

/**
 * 範囲の先頭の値で埋める（Ctrl+D は下方向、Ctrl+R は右方向）。
 * 下方向は各列の一番上の値、右方向は各行の一番左の値をコピーする。
 */
export function fillCells(selection: Range, direction: 'down' | 'right', valueAt: (r: number, c: number) => string): { r: number; c: number; value: string }[] {
  const out: { r: number; c: number; value: string }[] = []
  for (let r = selection.top; r <= selection.bottom; r++) {
    for (let c = selection.left; c <= selection.right; c++) {
      if (direction === 'down' && r > selection.top) out.push({ r, c, value: valueAt(selection.top, c) })
      if (direction === 'right' && c > selection.left) out.push({ r, c, value: valueAt(r, selection.left) })
    }
  }
  return out
}
