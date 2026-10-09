import { test } from '@playwright/test'
import { adminEmail, login } from '../e2e/helpers'
import { demoAccounts, demoPassword, seedDemo } from './demo-data'

// 研修・試用の環境にデモのデータを入れる（scripts/demo.sh、docs/demo.md）。画像は撮らない
test('デモのデータ', async ({ page }) => {
  await login(page)
  await seedDemo(page)
  console.log(['', 'デモのデータを入れました。ログインできるアカウント:', `  FP&A: ${adminEmail}`, ...demoAccounts.map((a) => `  ${a.role}: ${a.email}（${a.name}）`), `  （FP&A 以外のパスワード: ${demoPassword}）`].join('\n'))
})
