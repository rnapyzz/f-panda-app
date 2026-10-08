import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('使い方: ロールに合った手引きを開き、画面の「?」から節へ移れる', async ({ page }) => {
  await login(page)
  // FP&A は FP&A の手引きから開く
  await page.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link', { name: '使い方' }).click()
  await expect(page).toHaveURL(/\/manual\/fpa$/)
  const article = page.getByRole('article', { name: 'FP&Aの手引き' })
  await expect(article.getByRole('heading', { name: 'FP&A の手引き', level: 1 })).toBeVisible()
  // 画像が読み込める
  const image = article.getByRole('img', { name: /今月の作業/ })
  await expect(image).toBeVisible()
  expect(await image.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true)

  // ほかの手引きと目次
  await page.getByRole('navigation', { name: '手引き' }).getByRole('link', { name: /現場担当/ }).click()
  await expect(page.getByRole('heading', { name: '現場担当の手引き', level: 1 })).toBeVisible()
  await page.getByRole('list', { name: '目次' }).getByRole('link', { name: '今回の見込の説明と完了' }).click()
  await expect(page).toHaveURL(/#note$/)

  // 手引きの中のリンクで、別の手引きへ
  await page.getByRole('navigation', { name: '手引き' }).getByRole('link', { name: 'はじめに' }).click()
  await page.getByRole('article').getByRole('link', { name: 'FP&A の手引き' }).click()
  await expect(page).toHaveURL(/\/manual\/fpa$/)

  // 画面の「?」から、手引きの節へ
  await page.goto('/admin/scenarios')
  await page.goto('/')
  await page.getByRole('button', { name: '説明を見る' }).first().click()
  await page.getByRole('link', { name: '使い方を見る →' }).click()
  await expect(page).toHaveURL(/\/manual\/fpa#cycle$/)
  await expect(page.getByRole('heading', { name: '月次のサイクル（今月の作業）' })).toBeInViewport()
})
