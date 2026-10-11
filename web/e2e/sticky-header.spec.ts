import { expect, test } from '@playwright/test'
import { createActivity, login, seedMasters } from './helpers'

test('表を下へスクロールしても、見出しの行が画面の上に残る', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 500 })
  await login(page)
  const f = await seedMasters(page)
  for (let i = 0; i < 12; i++) await createActivity(page, f, { code: `STK-${f.run}-${i}`, name: `E2E見出し ${f.run} ${i}` })

  await page.goto(`/activities?q=${encodeURIComponent(`E2E見出し ${f.run}`)}`)
  const header = page.getByRole('columnheader', { name: '施策名' })
  await expect(page.getByRole('link', { name: `E2E見出し ${f.run} 11` })).toBeVisible()
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  // 見出しは画面の上端に残り、表の行はその下に隠れる
  await expect.poll(async () => Math.round((await header.boundingBox())!.y)).toBe(0)
  await expect(header).toBeInViewport()
})
