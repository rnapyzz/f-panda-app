import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters, showUnit } from './helpers'

test('予実比較で、基準との差異をセグメント → ユニット → 施策とたどれる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const budget = await a.post('/scenarios', { name: `E2E予算 ${f.run}`, fiscal_year: 2026 })
  await a.put(`/scenarios/${budget.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, target_month: '2026-04', amount: 1000000 },
      { subject_id: f.expenseId, target_month: '2026-04', amount: 400000 },
    ],
  })
  const forecast = await a.post('/scenarios', { name: `E2E見込 ${f.run}`, fiscal_year: 2026, base_scenario_id: budget.id })
  await a.put(`/scenarios/${forecast.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [{ subject_id: f.revenueId, target_month: '2026-04', amount: 1100000 }],
  })

  await page.goto(`/reports?fy=2026&base=${budget.id}&cmp=${forecast.id}&measure=profit&period=year`)
  const segment = page.getByRole('row', { name: new RegExp(`E2E事業 ${f.run}`) })
  // 金額の単位: 開くと百万円（小数点以下1桁）。円に切り替えて確かめる
  await expect(segment).toContainText('0.6')
  await showUnit(page, '円')
  // 利益: 予算 600,000 → 見込 700,000（+100,000、+16.6%）
  await expect(segment).toContainText('600,000')
  await expect(segment).toContainText('700,000')
  await expect(segment).toContainText('+100,000')

  await page.getByRole('button', { name: `E2E事業 ${f.run}を開く` }).click()
  await page.getByRole('button', { name: `E2E課 ${f.run}を開く` }).click()
  const activityRow = page.getByRole('row', { name: new RegExp(`${activity.name}`) })
  await expect(activityRow).toContainText('+100,000')
})
