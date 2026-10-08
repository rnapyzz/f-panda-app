// 手引き（docs/plan.md「2.21」）の Markdown を、表示用の構造に変換する。外部ライブラリは使わない。
//
// 対応する書き方（手引きで使うものだけ）:
//   # タイトル / ## 見出し {#id} / ### 小見出し {#id}（{#id} を付けると、その ID でリンクできる）
//   段落（連続する行は1つの段落）、- 箇条書き、1. 番号付き、| 表 |、> 補足、![説明](画像の URL)（1行だけのとき）
//   行内: **強調**、`コード`、[文字](リンク先)

export type Inline = { t: 'text'; v: string } | { t: 'strong'; c: Inline[] } | { t: 'code'; v: string } | { t: 'link'; href: string; c: Inline[] }

export type Block =
  | { t: 'title'; c: Inline[] }
  | { t: 'h'; level: 2 | 3; id: string; text: string; c: Inline[] }
  | { t: 'p'; c: Inline[] }
  | { t: 'ul' | 'ol'; items: Inline[][] }
  | { t: 'table'; head: Inline[][]; rows: Inline[][][] }
  | { t: 'note'; c: Inline[] }
  | { t: 'img'; alt: string; src: string }

/** 行内の書き方を変換する */
export function parseInline(s: string): Inline[] {
  const out: Inline[] = []
  const re = /\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)/g
  let last = 0
  for (let m = re.exec(s); m; m = re.exec(s)) {
    if (m.index > last) out.push({ t: 'text', v: s.slice(last, m.index) })
    if (m[1] !== undefined) out.push({ t: 'strong', c: parseInline(m[1]) })
    else if (m[2] !== undefined) out.push({ t: 'code', v: m[2] })
    else out.push({ t: 'link', href: m[4], c: parseInline(m[3]) })
    last = re.lastIndex
  }
  if (last < s.length) out.push({ t: 'text', v: s.slice(last) })
  return out
}

const cells = (line: string) =>
  line
    .trim()
    .replace(/^\||\|$/g, '')
    .split('|')
    .map((c) => parseInline(c.trim()))

/** Markdown を表示用のブロックに変換する */
export function parseMarkdown(src: string): Block[] {
  const lines = src.replace(/\r\n/g, '\n').split('\n')
  const out: Block[] = []
  let para: string[] = []
  const flush = () => {
    if (para.length) out.push({ t: 'p', c: parseInline(para.join('')) })
    para = []
  }
  let n = 0
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const trimmed = line.trim()
    let m: RegExpMatchArray | null
    if (trimmed === '') {
      flush()
    } else if ((m = trimmed.match(/^(#{1,3})\s+(.+?)(?:\s+\{#([\w-]+)\})?$/))) {
      flush()
      if (m[1].length === 1) out.push({ t: 'title', c: parseInline(m[2]) })
      else out.push({ t: 'h', level: m[1].length as 2 | 3, id: m[3] ?? `s${++n}`, text: m[2], c: parseInline(m[2]) })
    } else if ((m = trimmed.match(/^!\[([^\]]*)\]\(([^)\s]+)\)$/))) {
      flush()
      out.push({ t: 'img', alt: m[1], src: m[2] })
    } else if (/^[-*]\s+/.test(trimmed) || /^\d+\.\s+/.test(trimmed)) {
      flush()
      const ordered = /^\d+\./.test(trimmed)
      const items: Inline[][] = []
      for (; i < lines.length && (ordered ? /^\s*\d+\.\s+/ : /^\s*[-*]\s+/).test(lines[i]); i++) {
        items.push(parseInline(lines[i].trim().replace(/^([-*]|\d+\.)\s+/, '')))
      }
      i--
      out.push({ t: ordered ? 'ol' : 'ul', items })
    } else if (trimmed.startsWith('|')) {
      flush()
      const head = cells(trimmed)
      const rows: Inline[][][] = []
      i += 2 // 区切りの行を飛ばす
      for (; i < lines.length && lines[i].trim().startsWith('|'); i++) rows.push(cells(lines[i]))
      i--
      out.push({ t: 'table', head, rows })
    } else if (trimmed.startsWith('>')) {
      flush()
      const text: string[] = []
      for (; i < lines.length && lines[i].trim().startsWith('>'); i++) text.push(lines[i].trim().replace(/^>\s?/, ''))
      i--
      out.push({ t: 'note', c: parseInline(text.join('')) })
    } else {
      para.push(trimmed)
    }
  }
  flush()
  return out
}

/** 目次（## と ### の見出し） */
export function toc(blocks: Block[]): { id: string; text: string; level: 2 | 3 }[] {
  return blocks.flatMap((b) => (b.t === 'h' ? [{ id: b.id, text: b.text.replace(/\*\*|`/g, ''), level: b.level }] : []))
}
