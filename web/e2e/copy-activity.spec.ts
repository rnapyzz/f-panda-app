import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('既存の施策をコピーして、内訳とドライバーの定義を写した施策を作れる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const source = await createActivity(page, f, { name: `E2Eコピー元 ${f.run}`, activity_type: 'project', start_date: '2026-04-01', end_date: '2026-12-31' })
  await a.post(`/activities/${source.id}/drivers`, { code: 'hours', name: `工数 ${f.run}`, driver_kind: 'kpi', unit: '時間' })
  await a.post(`/activities/${source.id}/lines`, { subject_id: f.revenueId, name: `開発費 ${f.run}`, expression: 'hours * 10000', formula_enabled: true, reason: 'E2E' })

  await page.goto('/activities')
  await page.getByRole('button', { name: '＋ 施策を追加' }).click()
  const dialog = page.getByRole('dialog', { name: '施策の追加' })
  await dialog.getByLabel('既存の施策をコピー（任意）').selectOption({ label: `${source.code} ${source.name}` })
  await expect(dialog.getByLabel('施策名')).toHaveValue(`${source.name}（コピー）`)
  await dialog.getByLabel('施策名').fill(`E2Eコピー先 ${f.run}`)
  await dialog.getByLabel('開始日').fill('2027-01-01')
  await dialog.getByLabel('終了日').fill('2027-06-30')
  await dialog.getByRole('button', { name: '保存' }).click()

  // 「設定」タブで、写した内訳とドライバーを確かめられる
  await expect(page).toHaveURL(/tab=settings/)
  await expect(page.getByRole('heading', { name: `E2Eコピー先 ${f.run}` })).toBeVisible()
  await expect(page.getByText(`開発費 ${f.run}`)).toBeVisible()
  await expect(page.getByText(`工数 ${f.run}`)).toBeVisible()
})
