import { expect, test, type Page } from '@playwright/test'
import { login } from '../e2e/helpers'
import { demoPassword, seedDemo } from './demo-data'

// 手引き（src/manual/*.md）に載せる画像を撮る。データは研修用と同じデモのデータ（demo-data.ts）。
const out = (name: string) => `public/manual/${name}.png`

test('手引きの画像', async ({ page, browser }) => {
  await login(page)
  const d = await seedDemo(page)

  const shot = async (p: Page, name: string) => {
    await p.waitForLoadState('networkidle')
    await p.screenshot({ path: out(name) })
  }

  // FP&A のホーム（今月の作業）
  await page.goto('/')
  await expect(page.getByRole('list', { name: 'サイクルのチェックリスト' })).toBeVisible()
  await shot(page, 'fpa-work')

  // 施策の一覧（ポートフォリオ・マップ）
  await page.goto('/activities?view=portfolio')
  await expect(page.getByRole('group', { name: /ポートフォリオ・マップ/ })).toBeVisible()
  await page.waitForLoadState('networkidle')
  // 図のカード（凡例と図）だけを撮る
  await page.locator('div:has(> [data-chart])').screenshot({ path: out('activity-portfolio') })
  await page.goto('/activities?view=pipeline')
  await expect(page.getByRole('group', { name: /確度の推移/ })).toBeVisible()
  await page.waitForLoadState('networkidle')
  await page.locator('div:has(> [data-chart])').screenshot({ path: out('activity-pipeline') })

  // リスク画面
  await page.goto('/risks')
  await expect(page.getByRole('img', { name: /悲観/ }).first()).toBeVisible()
  await shot(page, 'risk')

  // 担当者（鈴木）のホームと施策の画面
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: 'ja-JP', timezoneId: 'Asia/Tokyo' })
  const mp = await context.newPage()
  await login(mp, 'suzuki@example.com', demoPassword)
  await expect(mp.getByRole('heading', { name: /更新する施策/ })).toBeVisible()
  await shot(mp, 'home-member')
  await mp.goto(`/activities/${d.activityId}?tab=update&scenario=${d.scenarioId}`)
  await expect(mp.getByRole('table', { name: '目標・前回の見込との比較' })).toBeVisible()
  await shot(mp, 'activity-update')
  const note = mp.locator('section', { has: mp.getByRole('heading', { name: '今回の見込の説明' }) })
  await mp.getByLabel('今回の見込の説明').fill('A社の検収が 12月から 1月にずれたため、Q3 の売上が前回の見込を下回る。年間では目標どおり。')
  await mp.getByText('時期のずれ', { exact: true }).click()
  await note.scrollIntoViewIfNeeded()
  await note.screenshot({ path: out('activity-note') })
  await context.close()
})
