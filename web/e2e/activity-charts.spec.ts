import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// ほかのテストの施策もあるので、キーワードで自分の施策に絞り込む
test('施策の一覧を図（6種類）で見られ、図から施策を開ける', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f, { name: `E2E図 ${f.run}`, start_date: '2026-06-01', end_date: '2027-02-28' })
  // 図は今回の見込を使うので、2026年度の版を作って今回の見込にする（ホームのテストなどと同じく、作成中を切り替える）
  const scenario = await a.post('/scenarios', { name: `E2E図 ${f.run}`, fiscal_year: 2026 })
  await a.post(`/scenarios/${scenario.id}/activate`, {})
  const res = await page.request.put(`/api/scenarios/${scenario.id}/activities/${activity.id}/amounts`, {
    data: { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: '2027-03', amount: 1000000 }] },
  })
  expect(res.ok()).toBeTruthy()

  await page.goto(`/activities?q=${activity.code}`)
  await page.getByRole('group', { name: '表示' }).getByRole('button', { name: 'ポートフォリオ' }).click()
  await expect(page).toHaveURL(/view=portfolio/)
  const bubble = page.getByRole('link', { name: new RegExp(`^E2E図 ${f.run}: 確度の割合 100%`) })
  await bubble.hover()
  await expect(page.getByRole('tooltip')).toContainText('売上（満額）')

  const view = (name: string) => page.getByRole('group', { name: '表示' }).getByRole('button', { name, exact: true }).click()

  await view('振れ幅')
  await expect(page.getByRole('link', { name: new RegExp(`^E2E図 ${f.run}: 悲観`) })).toBeVisible()
  await view('増減の内訳')
  await expect(page.getByRole('img', { name: /^今回の見込: / })).toBeVisible()
  await expect(page.getByRole('link', { name: new RegExp(`^E2E図 ${f.run}: \\+1,000,000 円`) })).toBeVisible()
  await view('スケジュール')
  await expect(page.getByRole('link', { name: new RegExp(`^E2E図 ${f.run}: 2026-06-01 〜 2027-02-28`) })).toBeVisible()
  await view('確度の推移')
  await expect(page.getByRole('img', { name: /^3月: 売上 1,000,000 円/ })).toBeVisible()

  await page.getByRole('group', { name: '表示' }).getByRole('button', { name: 'ツリーマップ' }).click()
  const tile = page.getByRole('link', { name: new RegExp(`^E2E図 ${f.run}: 売上 1,000,000 円`) })
  await tile.click()
  await expect(page).toHaveURL(new RegExp(`/activities/${activity.id}$`))
})
