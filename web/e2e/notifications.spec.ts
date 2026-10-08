import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('作成中のシナリオに締切を入れると、担当者にお知らせが届き、ベルから開ける。通知の設定を変えられる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const me = (await (await page.request.get('/api/auth/me')).json()).user as { id: number }
  await createActivity(page, f, { owner_user_id: me.id })
  const name = `E2E通知 ${f.run}`
  const scenario = await a.post('/scenarios', { name, fiscal_year: 2026 })
  await a.post(`/scenarios/${scenario.id}/activate`, {})

  // シナリオ管理の「設定」で締切を入れる
  await page.goto('/admin/scenarios')
  await page.getByRole('combobox', { name: '年度' }).selectOption('2026')
  await page.getByRole('button', { name: `${name}の設定` }).click()
  const settings = page.getByRole('dialog', { name: 'シナリオの設定' })
  await settings.getByLabel('現場の更新の締切日').fill('2099-12-25')
  await settings.getByRole('button', { name: '保存' }).click()
  await expect(settings).toHaveCount(0)

  // 作成中のバーに締切、ベルに未読
  const bar = page.getByRole('status', { name: '今回の見込' })
  await page.reload()
  await expect(bar.getByLabel('更新の締切')).toContainText('締切 12/25')
  const bell = bar.getByRole('button', { name: /お知らせ（未読 \d+ 件）/ })
  await expect(bell).toBeVisible()
  await bell.click()
  const panel = page.getByRole('dialog', { name: 'お知らせ' })
  const item = panel.getByRole('button', { name: new RegExp(`見込の更新が始まりました（${name}）`) })
  await expect(item).toContainText('締切は 12/25')
  await item.click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'ホーム' })).toBeVisible()

  // 通知の設定: 日数と時刻を変えて保存する。Slack は未設定。送信の記録に「更新の開始」が残る
  await page.goto('/admin/notifications')
  await page.getByLabel('締切の何日前に知らせるか').fill('5,1')
  await page.getByLabel('送信時刻（日本時間）').fill('08:30')
  await page.getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('status').filter({ hasText: '保存しました' })).toBeVisible()
  await expect(page.getByText('Slack には送りません')).toBeVisible()
  await expect(page.getByRole('row', { name: new RegExp(name) })).toContainText('更新の開始')

  // 元に戻す（ほかのテストに影響しないように）
  await a.put('/notification-settings', { enabled_kinds: {}, reminder_days: [3, 1], send_time: '09:00' })
})
