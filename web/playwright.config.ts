import { defineConfig, devices } from '@playwright/test'

// E2E テストは、起動済みのアプリ（docker compose）に対して実行する。
// 通常は scripts/e2e.sh が専用のスタックを起動してから実行する。
export default defineConfig({
  testDir: './e2e',
  // テストは同じ DB を使うので、順番に1つずつ実行する
  workers: 1,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  timeout: 30_000,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:18080',
    locale: 'ja-JP',
    timezoneId: 'Asia/Tokyo',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1400, height: 900 } } }],
})
