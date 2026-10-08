import { expect, test, type Locator, type Page } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

/** P/L 表の、項目（label）の行グループ。行は 基準・最新・差異 の順 */
function group(page: Page, label: string): Locator {
  return page.getByRole('table', { name: 'P/L' }).locator('tbody').filter({ has: page.getByText(label, { exact: true }) })
}

/** 行（0: 基準、1: 最新、2: 差異）の金額のセル。先頭の「基準」などのラベルは除く */
function amounts(g: Locator, row: number): Locator {
  return g.locator('tr').nth(row).locator('td').filter({ hasNotText: /^(目標|最新|差異)$/ })
}

test('施策詳細の P/L で、基準と最新の差異を期間を切り替えて見られ、科目・内訳へドリルダウンできる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const budget = await a.post('/scenarios', { name: `E2E予算 ${f.run}`, fiscal_year: 2026 })
  const forecast = await a.post('/scenarios', { name: `E2E見込 ${f.run}`, fiscal_year: 2026 })
  const line = await a.post(`/activities/${activity.id}/lines`, { subject_id: f.revenueId, name: '月額利用料' })
  await a.put(`/scenarios/${budget.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, target_month: '2026-04', amount: 1000000 },
      { subject_id: f.expenseId, target_month: '2026-04', amount: 400000 },
    ],
  })
  await a.put(`/scenarios/${forecast.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, line_id: line.id, target_month: '2026-04', amount: 900000 },
      { subject_id: f.revenueId, target_month: '2026-05', amount: 300000 },
      { subject_id: f.expenseId, target_month: '2026-04', amount: 450000 },
    ],
  })

  await page.goto(`/activities/${activity.id}?tab=overview`)
  await page.getByRole('combobox', { name: '年度' }).selectOption('2026')
  await page.getByRole('combobox', { name: '目標のシナリオ' }).selectOption(String(budget.id))
  await page.getByRole('combobox', { name: '最新のシナリオ' }).selectOption(String(forecast.id))

  // 既定は四半期（Q1〜Q4・通期）
  await expect(page.getByRole('button', { name: '四半期', pressed: true })).toBeVisible()
  await expect(amounts(group(page, '収益'), 0).first()).toHaveText('1,000,000')
  await expect(amounts(group(page, '収益'), 1).first()).toHaveText('1,200,000')
  await expect(amounts(group(page, '収益'), 2).first()).toHaveText('+200,000')
  await expect(amounts(group(page, '費用'), 2).first()).toHaveText('+50,000')
  await expect(amounts(group(page, '利益'), 2).last()).toHaveText('+150,000(+25%)')

  // 月次に切り替えると 5月の列が見える
  await page.getByRole('button', { name: '月次' }).click()
  await expect(amounts(group(page, '収益'), 1).nth(1)).toHaveText('300,000')
  await page.getByRole('button', { name: '通期' }).click()
  await expect(amounts(group(page, '収益'), 1)).toHaveCount(1)

  // 収益 → 科目 → 内訳とドリルダウン
  await page.getByRole('button', { name: '収益を開く' }).click()
  await page.getByRole('button', { name: `${f.revenueName}を開く` }).click()
  await expect(amounts(group(page, '月額利用料'), 1)).toHaveText(['900,000'])
  await expect(amounts(group(page, 'その他'), 0)).toHaveText(['1,000,000'])
  await expect(amounts(group(page, 'その他'), 1)).toHaveText(['300,000'])
  await page.getByRole('button', { name: '収益を閉じる' }).click()
  await expect(group(page, '月額利用料')).toHaveCount(0)
})
