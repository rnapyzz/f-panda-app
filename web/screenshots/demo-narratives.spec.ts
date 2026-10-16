import { test } from '@playwright/test'
import { login } from '../e2e/helpers'
import { seedDemoNarratives } from './demo-data'

// デモのデータを入れ済みの環境に、シナリオの説明だけを足す（scripts/demo.sh narratives、docs/demo.md）。
// 説明がまだなく、ロックされていないデモのシナリオにだけ入れる。何度実行しても同じ結果になる
test('デモのシナリオの説明', async ({ page }) => {
  await login(page)
  const written = await seedDemoNarratives(page)
  console.log(`\nデモのシナリオの説明: ${written} 件を入れました（説明があるもの・ロック済みのものはそのまま）`)
})
