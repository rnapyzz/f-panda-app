import assert from 'node:assert/strict'
import { test } from 'node:test'
import { cleanNumber, fillCells, move, parseClipboard, pasteCells, rangeOf, toTsv } from './sheet.ts'

test('cleanNumber: 桁区切り・円記号・全角・会計表記のマイナスをそろえる', () => {
  assert.equal(cleanNumber('1,234,567'), '1234567')
  assert.equal(cleanNumber(' ¥50,000 '), '50000')
  assert.equal(cleanNumber('12,000円'), '12000')
  assert.equal(cleanNumber('１２，０００'), '12000')
  assert.equal(cleanNumber('－５００'), '-500')
  assert.equal(cleanNumber('△1,000'), '-1000')
  assert.equal(cleanNumber('▲1,000'), '-1000')
  assert.equal(cleanNumber('(1,000)'), '-1000')
  assert.equal(cleanNumber('52000.5'), '52000.5')
  assert.equal(cleanNumber('+300'), '300')
  assert.equal(cleanNumber(''), '')
  // 数値として読めないものはそのまま（サーバーの検証でエラーを出す）
  assert.equal(cleanNumber(' abc '), 'abc')
  assert.equal(cleanNumber('1-2'), '1-2')
})

test('parseClipboard: タブ区切り・改行・引用符', () => {
  assert.deepEqual(parseClipboard('1\t2\t3\r\n4\t5\t6\r\n'), [
    ['1', '2', '3'],
    ['4', '5', '6'],
  ])
  assert.deepEqual(parseClipboard('100'), [['100']])
  assert.deepEqual(parseClipboard('"1,000"\t"a\nb"\t"say ""hi"""'), [['1,000', 'a\nb', 'say "hi"']])
  assert.deepEqual(parseClipboard('1\t\t3'), [['1', '', '3']])
  assert.deepEqual(parseClipboard(''), [])
})

test('toTsv: parseClipboard で元に戻せる', () => {
  const m = [
    ['1000', ''],
    ['a\tb', 'x"y'],
  ]
  assert.deepEqual(parseClipboard(toTsv(m)), m)
})

test('move: グリッドの外に出ない。Ctrl+矢印で端まで', () => {
  assert.deepEqual(move({ r: 0, c: 0 }, -1, 0, 3, 12), { r: 0, c: 0 })
  assert.deepEqual(move({ r: 1, c: 5 }, 0, 1, 3, 12), { r: 1, c: 6 })
  assert.deepEqual(move({ r: 1, c: 5 }, 0, 1, 3, 12, true), { r: 1, c: 11 })
  assert.deepEqual(move({ r: 1, c: 5 }, 1, 0, 3, 12, true), { r: 2, c: 5 })
})

test('pasteCells: 左上から貼り付け、はみ出しは捨てる', () => {
  const cells = pasteCells(rangeOf({ r: 1, c: 10 }, { r: 1, c: 10 }), [['1', '2', '3']], 3, 12)
  assert.deepEqual(cells, [
    { r: 1, c: 10, value: '1' },
    { r: 1, c: 11, value: '2' },
  ])
})

test('pasteCells: 1つの値は選択範囲全体に貼り付ける', () => {
  const cells = pasteCells(rangeOf({ r: 0, c: 0 }, { r: 1, c: 1 }), [['5']], 3, 12)
  assert.equal(cells.length, 4)
  assert.ok(cells.every((c) => c.value === '5'))
})

test('fillCells: 右方向は各行の左端、下方向は各列の上端の値で埋める', () => {
  const grid = [
    ['1', '2', '3'],
    ['4', '5', '6'],
  ]
  const at = (r: number, c: number) => grid[r][c]
  assert.deepEqual(fillCells(rangeOf({ r: 0, c: 0 }, { r: 1, c: 2 }), 'right', at), [
    { r: 0, c: 1, value: '1' },
    { r: 0, c: 2, value: '1' },
    { r: 1, c: 1, value: '4' },
    { r: 1, c: 2, value: '4' },
  ])
  assert.deepEqual(fillCells(rangeOf({ r: 0, c: 1 }, { r: 1, c: 2 }), 'down', at), [
    { r: 1, c: 1, value: '2' },
    { r: 1, c: 2, value: '3' },
  ])
})
