import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatAmount, formatNumber, formatSignedAmount, formatPercent, formatRate, formatSignedYen, formatYen, monthLabel, yearMonthLabel } from './format.ts'

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

test('formatAmount: 百万円は小数点以下1桁、千円は整数（四捨五入）', () => {
  assert.equal(formatAmount(158_940_000n, 'million'), '158.9')
  assert.equal(formatAmount(158_950_000n, 'million'), '159.0')
  assert.equal(formatAmount(-6_250_000n, 'million'), '▲6.3')
  assert.equal(formatAmount(40_000n, 'million'), '0.0')
  assert.equal(formatAmount(1_234_567_890_000n, 'million'), '1,234,567.9')
  assert.equal(formatAmount('158940500', 'thousand'), '158,941')
  assert.equal(formatAmount(-1499, 'thousand'), '▲1')
  assert.equal(formatAmount(1234, 'yen'), '1,234')
  assert.equal(formatSignedAmount(3_400_000n, 'million'), '+3.4')
  assert.equal(formatSignedAmount(-3_400_000n, 'million'), '▲3.4')
  assert.equal(formatSignedAmount(10_000n, 'million'), '+0.0')
  assert.equal(formatSignedAmount(-10_000n, 'million'), '▲0.0')
  assert.equal(formatSignedAmount(0n, 'million'), '0.0')
  assert.equal(formatAmount(-400n, 'thousand'), '▲0')
})
