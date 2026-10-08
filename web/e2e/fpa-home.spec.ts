import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// ほかのテストと実績の月が重ならないよう、2036年度を使う
test('FP&A のホームの「今月の作業」で、サイクルの進み具合と更新の状況を確認し、担当者に催促できる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const member = await a.post('/users', { name: `E2E催促先 ${f.run}`, email: `e2e-remind-${f.run.toLowerCase()}@example.com`, role: 'member', password: 'e2e-member-password' })
  const activity = await createActivity(page, f, { owner_user_id: member.id })
  const csv = `target_month,box_code,account_code,amount\n2036-04,${activity.code},R${f.run},500000\n`
  const res = await page.request.post('/api/actuals/import', { multipart: { file: { name: 'a.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) }, reason: 'E2E: 4月実績' } })
  expect(res.ok()).toBeTruthy()
  const name = `E2E月次 ${f.run}`
  const scenario = await a.post('/scenarios', { name, fiscal_year: 2036, actual_through: '2036-04' })
  await a.post(`/scenarios/${scenario.id}/activate`, {})
  await a.put(`/scenarios/${scenario.id}`, { name, actual_through: '2036-04', update_deadline: '2036-05-15', reason: 'E2E' })

  await page.goto('/')
  const checklist = page.getByRole('list', { name: 'サイクルのチェックリスト' })
  await expect(checklist.getByRole('listitem')).toHaveCount(5)
  await expect(checklist.getByRole('listitem').nth(0)).toContainText('4月の実績を取り込む')
  await expect(checklist.getByRole('listitem').nth(0)).toContainText('済み')
  // 実績・未割当・月次の見込・締切は済み、今は現場の更新
  const current = checklist.locator('[aria-current="step"]')
  await expect(current).toContainText('現場の更新')
  await expect(current).toContainText(/完了 \d+ \/ \d+ 件/) // ほかのテストの施策も更新の対象になる

  // ユニット別と未完了の担当者。催促は1日1回
  await expect(page.getByRole('region', { name: 'ユニット別' }).getByRole('row', { name: new RegExp(`E2E課 ${f.run}`) })).toContainText('0%')
  const owners = page.getByRole('region', { name: '未完了の担当者' })
  await owners.getByRole('button', { name: `E2E催促先 ${f.run}に催促する` }).click()
  await expect(owners.getByRole('row', { name: new RegExp(`E2E催促先 ${f.run}`) })).toContainText('本日送信済み')
})
