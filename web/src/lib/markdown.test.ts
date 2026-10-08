import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseInline, parseMarkdown, toc } from './markdown.ts'

test('parseInline: 強調・コード・リンク', () => {
  assert.deepEqual(parseInline('「**保存**」を押す。`reason` は[こちら](/manual/fpa#cycle)'), [
    { t: 'text', v: '「' },
    { t: 'strong', c: [{ t: 'text', v: '保存' }] },
    { t: 'text', v: '」を押す。' },
    { t: 'code', v: 'reason' },
    { t: 'text', v: ' は' },
    { t: 'link', href: '/manual/fpa#cycle', c: [{ t: 'text', v: 'こちら' }] },
  ])
})

test('parseMarkdown: 見出し・段落・箇条書き・表・補足・画像', () => {
  const md = [
    '# 現場担当の手引き',
    '',
    '## 毎月の更新 {#update}',
    '1行目',
    '2行目',
    '',
    '- A',
    '- B',
    '1. 一',
    '2. 二',
    '',
    '| 列1 | 列2 |',
    '| --- | --- |',
    '| a | **b** |',
    '> 補足です',
    '![ホーム](/manual/home.png)',
    '### 小見出し',
  ].join('\n')
  const blocks = parseMarkdown(md)
  assert.deepEqual(
    blocks.map((b) => b.t),
    ['title', 'h', 'p', 'ul', 'ol', 'table', 'note', 'img', 'h'],
  )
  assert.deepEqual(blocks[2], { t: 'p', c: [{ t: 'text', v: '1行目2行目' }] })
  const table = blocks[5]
  assert.ok(table.t === 'table' && table.rows.length === 1 && table.rows[0][1][0].t === 'strong')
  assert.deepEqual(toc(blocks), [
    { id: 'update', text: '毎月の更新', level: 2 },
    { id: 's1', text: '小見出し', level: 3 },
  ])
})
