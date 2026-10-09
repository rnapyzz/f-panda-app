import { defineConfig, devices } from '@playwright/test'

// 手引きの画像（docs/plan.md「2.21」）を撮る。通常の E2E とは別に、`make manual-screenshots` で実行する。
// scripts/e2e.sh が起動した空の専用スタックにデモのデータを入れて、public/manual/ に画像を書き出す。
export default defineConfig({
  testDir: './screenshots',
  // 研修用のデータを入れるだけの demo.spec.ts は、playwright.demo.config.ts で実行する
  testMatch: 'manual.spec.ts',
  workers: 1,
  timeout: 120_000,
  reporter: 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:18080',
    locale: 'ja-JP',
    timezoneId: 'Asia/Tokyo',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1 } }],
})
