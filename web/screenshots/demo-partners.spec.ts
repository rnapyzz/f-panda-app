import { test } from '@playwright/test'
import { login } from '../e2e/helpers'
import { seedDemoPartners } from './demo-data'

// デモのデータを入れ済みの環境に、取引先と内訳への紐づけだけを足す（scripts/demo.sh partners、docs/demo.md）。
// 何度実行しても同じ結果になる。デモの施策がない環境では、取引先だけを入れる
test('デモの取引先', async ({ page }) => {
  await login(page)
  const { created, linked } = await seedDemoPartners(page)
  console.log(`\nデモの取引先: ${created} 件を追加し、${linked} 件の内訳に付けました（すでにあるものはそのまま）`)
})
