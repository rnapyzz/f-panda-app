import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, makeCurrent, seedMasters } from './helpers'

// ほかのテストと年度が重ならないよう、2042年度を使う
test('年度をまたぐ施策は、当期の見込で翌年度の月も入力でき、概要に施策全体の合計が出る', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  // 10月着手・翌年6月納品のプロジェクト
  const activity = await createActivity(page, f, { activity_type: 'project', start_date: '2042-10-01', end_date: '2043-06-30' })
  const scenario = await a.post('/scenarios', { name: `E2E2042見込 ${f.run}`, fiscal_year: 2042, plan_role: 'latest' })
  await makeCurrent(page, scenario.id)
  await a.put(`/scenarios/${scenario.id}/activities/${activity.id}/amounts`, { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: '2042-12', amount: 1000000 }] })

  await page.goto(`/activities/${activity.id}?tab=update&scenario=${scenario.id}`)
  // 当期の12か月の右に、翌年度の月の列と「翌年度の計」が出る
  await expect(page.getByRole('columnheader', { name: /翌年度（2043年度）/ }).first()).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '翌年度の計' })).toBeVisible()
  await gridCell(page, `${f.revenueName} 翌6月`).click()
  await page.keyboard.type('2000000')
  await page.keyboard.press('Enter')
  await page.getByLabel('変更理由', { exact: true }).fill('E2E: 6月の納品分')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('1 件を保存しました')).toBeVisible()
  await expect(gridCell(page, `${f.revenueName} 翌6月`)).toHaveAccessibleName(`${f.revenueName} 翌6月: 2,000,000`)

  // 概要の P/L: 施策全体（当期 1,000,000 + 翌年度 2,000,000）
  await page.getByRole('tab', { name: '概要' }).click()
  await page.getByLabel('年度', { exact: true }).selectOption('2042')
  await page.getByRole('radiogroup', { name: '金額の単位' }).first().getByRole('radio', { name: '円', exact: true }).click()
  const whole = page.getByRole('region', { name: '施策全体' })
  await expect(whole.getByRole('row', { name: /^売上/ })).toContainText('1,000,000')
  await expect(whole.getByRole('row', { name: /^売上/ })).toContainText('2,000,000')
  await expect(whole.getByRole('row', { name: /^売上/ })).toContainText('3,000,000')
})
