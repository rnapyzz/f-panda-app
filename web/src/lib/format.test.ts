import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatNumber, formatPercent, formatRate, formatSignedYen, formatYen, monthLabel, yearMonthLabel } from './format.ts'

test('formatYen: 3桁区切り。大きな整数も桁落ちしない', () => {
  assert.equal(formatYen('1234567'), '1,234,567')
  assert.equal(formatYen(-500000), '▲500,000')
  assert.equal(formatYen('0'), '0')
  assert.equal(formatYen('123456789012345678'), '123,456,789,012,345,678')
  assert.equal(formatYen(null), '')
  assert.equal(formatYen(''), '')
})

test('formatNumber: 小数を保つ', () => {
  assert.equal(formatNumber('52000.5'), '52,000.5')
  assert.equal(formatNumber('-1200'), '▲1,200')
  assert.equal(formatNumber('-0.5'), '▲0.5')
  assert.equal(formatNumber('0.123456'), '0.123456')
})

test('formatPercent: 0〜1 をパーセントに', () => {
  assert.equal(formatPercent(0.6), '60%')
  assert.equal(formatPercent(0.1234), '12.3%')
  assert.equal(formatPercent(null), '—')
})

test('月の表示', () => {
  assert.equal(monthLabel('2027-01'), '1月')
  assert.equal(yearMonthLabel('2026-10'), '2026年10月')
})

test('formatSignedYen・formatRate', () => {
  assert.equal(formatSignedYen(1000n), '+1,000')
  assert.equal(formatSignedYen(-1000n), '▲1,000')
  assert.equal(formatSignedYen(0n), '0')
  assert.equal(formatRate(5.2), '+5.2%')
  assert.equal(formatRate(-5.2), '▲5.2%')
  assert.equal(formatRate(0), '0%')
})
