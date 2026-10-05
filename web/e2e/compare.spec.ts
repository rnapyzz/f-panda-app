import { expect, test, type Locator, type Page } from '@playwright/test'
import { api, createActivity, gridCell, login, seedMasters } from './helpers'

/** 比較の表の、項目（label）の行グループ。行は 今回・基準・基準との差・前回見込・前回見込との差 の順 */
function group(page: Page, label: string): Locator {
  return page.getByRole('table', { name: '基準・前回見込との比較' }).locator('tbody').filter({ has: page.getByText(label, { exact: true }) })
}
function firstAmount(g: Locator, row: number): Locator {
  return g.locator('tr').nth(row).locator('td').nth(1)
}

test('数値入力画面で、基準・前回見込との差を、保存前の入力も含めて確認できる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const amount = (sid: number, value: number) =>
    a.put(`/scenarios/${sid}/activities/${activity.id}/amounts`, { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: '2026-04', amount: value }] })

  // 基準（期初計画）→ 前回見込（複製）→ 今回（複製。前回見込は複製元になる）
  const budget = await a.post('/scenarios', { name: `E2E期初 ${f.run}`, fiscal_year: 2026, plan_role: 'initial' })
  await amount(budget.id, 1000000)
  const previous = await a.post('/scenarios', { name: `E2E前回 ${f.run}`, fiscal_year: 2026, base_scenario_id: budget.id })
  await amount(previous.id, 1100000)
  const current = await a.post('/scenarios', { name: `E2E今回 ${f.run}`, fiscal_year: 2026, base_scenario_id: previous.id })

  await page.goto(`/scenarios/${current.id}/activities/${activity.id}`)
  const revenue = group(page, '収益')
  await expect(firstAmount(revenue, 0)).toHaveText('1,100,000') // 今回 Q1
  await expect(firstAmount(revenue, 1)).toHaveText('1,000,000') // 基準
  await expect(firstAmount(revenue, 2)).toHaveText('+100,000')
  await expect(firstAmount(revenue, 3)).toHaveText('1,100,000') // 前回見込
  await expect(firstAmount(revenue, 4)).toHaveText('0')

  // 保存する前に、入力に合わせて差が動く
  await gridCell(page, `${f.revenueName} 4月`).click()
  await page.keyboard.type('1200000')
  await page.keyboard.press('Enter')
  await expect(firstAmount(revenue, 2)).toHaveText('+200,000')
  await expect(firstAmount(revenue, 4)).toHaveText('+100,000')
  await expect(page.getByText('1 件の変更')).toBeVisible()
})
