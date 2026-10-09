import { defineConfig } from '@playwright/test'

// 研修・試用の環境にデモのデータを入れる（scripts/demo.sh、docs/demo.md）。ブラウザは API のログインにだけ使う。
export default defineConfig({
  testDir: './screenshots',
  testMatch: 'demo.spec.ts',
  workers: 1,
  timeout: 180_000,
  reporter: 'list',
  use: { baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:18100', locale: 'ja-JP', timezoneId: 'Asia/Tokyo' },
})
