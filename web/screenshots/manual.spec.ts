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

  // ダッシュボード（全社）
  await page.goto('/dashboard')
  await expect(page.getByRole('region', { name: '年間の状況' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'どこで差が出ているか' }).getByRole('button').first()).toBeVisible()
  await shot(page, 'dashboard')

  // シナリオの全体像（今回の見込）
  await page.goto(`/scenarios/${d.scenarioId}`)
  await expect(page.getByRole('region', { name: '数字から見えること' })).toBeVisible()
  await expect(page.getByRole('region', { name: '何でできているか（売上）' }).getByRole('button').first()).toBeVisible()
  await shot(page, 'scenario-overview')
  // シナリオの説明を書く（FP&A）
  await page.getByRole('region', { name: 'このシナリオの説明' }).getByRole('button', { name: '編集' }).click()
  await expect(page.getByRole('complementary', { name: '書くときの手がかり' })).toBeVisible()
  await shot(page, 'scenario-narrative')

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
  // 検収月に一括の施策の「原価と仕掛」（概要の P/L の下）
  // 月の列（翌年の1月まで）が収まるよう、画面を広げて撮る。表は左右に余白がはみ出すので、少し広く切り取る
  await mp.setViewportSize({ width: 1800, height: 900 })
  await mp.goto(`/activities/${d.wipActivityId}?tab=overview`)
  const wip = mp.getByRole('region', { name: '原価と仕掛' })
  await expect(wip).toContainText('計上月 2027年1月')
  await mp.waitForLoadState('networkidle')
  await wip.scrollIntoViewIfNeeded()
  const box = (await wip.boundingBox())!
  await mp.screenshot({ path: out('activity-wip'), clip: { x: box.x - 24, y: box.y - 12, width: box.width + 48, height: box.height + 24 } })
  await context.close()
})
