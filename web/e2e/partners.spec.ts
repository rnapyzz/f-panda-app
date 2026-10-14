import { expect, test } from '@playwright/test'
import { api, createActivity, login, makeCurrent, seedMasters } from './helpers'

// ほかのテストと年度が重ならないよう、2046年度を使う
test('FP&A が取引先を登録し、内訳に付けると、今回の更新と取引先の画面に出る', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const company = `E2E商事 ${f.run}`
  const partnerName = `${company} 情報システム部`

  // 取引先を登録する（受注）。発注の取引先は収益の内訳には選べない
  await page.goto('/masters/partners')
  await page.getByRole('button', { name: '＋ 取引先を追加' }).click()
  const dialog = page.getByRole('dialog', { name: '取引先の追加' })
  await dialog.getByLabel('取引先名').fill(partnerName)
  await dialog.getByLabel('会社名').fill(company)
  await dialog.getByLabel('会計システムコード').fill(`C${f.run}`)
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole('row', { name: new RegExp(partnerName) })).toContainText('受注')
  await a.post('/partners', { name: `${company} 購買部`, company_name: company, direction: 'purchase' })

  // 内訳に付ける
  const activity = await createActivity(page, f)
  const scenario = await a.post('/scenarios', { name: `E2E2046見込 ${f.run}`, fiscal_year: 2046, plan_role: 'latest' })
  await makeCurrent(page, scenario.id)
  await a.post(`/activities/${activity.id}/lines`, { subject_id: f.revenueId, name: '保守' })
  await page.goto(`/activities/${activity.id}?tab=settings`)
  const lines = page.locator('section', { has: page.getByRole('heading', { name: '金額の内訳' }) })
  await lines.getByRole('row', { name: /保守/ }).getByRole('button', { name: '編集' }).click()
  const edit = page.getByRole('dialog', { name: '内訳の編集' })
  const select = edit.getByLabel('取引先（任意）')
  await expect(select.locator('option', { hasText: '購買部' })).toHaveCount(0)
  await select.selectOption({ label: `${partnerName}（${company}・受注）` })
  await edit.getByRole('button', { name: '保存' }).click()
  await expect(edit).toHaveCount(0)
  await expect(lines.getByRole('row', { name: /保守/ })).toContainText(partnerName)

  // 今回の更新の表の内訳の行に出る
  await page.goto(`/activities/${activity.id}?tab=update&scenario=${scenario.id}`)
  await expect(page.getByText(new RegExp(`／ ${partnerName}`))).toBeVisible()

  // 取引先の画面: 付いている施策と内訳
  await page.goto('/masters/partners')
  await page.getByRole('button', { name: partnerName }).click()
  const card = page.locator('section', { has: page.getByRole('heading', { name: new RegExp(`${partnerName}.*が付いた内訳`) }) })
  await expect(card.getByRole('row', { name: /保守/ })).toContainText(activity.name)
  await card.getByRole('link', { name: new RegExp(activity.name) }).click()
  await expect(page).toHaveURL(new RegExp(`/activities/${activity.id}`))
})
