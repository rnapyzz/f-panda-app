import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('ドライバー値を入力して保存すると、計算式の金額が算出される', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f, { calc_mode: 'formula' })
  await a.post(`/activities/${activity.id}/drivers`, { code: 'unit_price', name: '月額単価', driver_kind: 'value' })
  await a.post(`/activities/${activity.id}/drivers`, { code: 'customers', name: '契約社数', driver_kind: 'kpi' })
  await a.put(`/activities/${activity.id}/formulas/${f.revenueId}`, { expression: 'unit_price * customers', reason: 'E2E' })
  const scenario = await a.post('/scenarios', { name: `E2E予算 ${f.run}`, scenario_kind: 'budget', fiscal_year: 2026 })

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  await page.getByLabel('月額単価 4月').fill('50,000')
  await page.getByLabel('契約社数 4月').fill('12')
  await page.getByLabel('月額単価 5月').fill('52000.5')
  await page.getByLabel('契約社数 5月').fill('15')

  const save = page.getByRole('button', { name: '保存', exact: true })
  await expect(page.getByText('4 件の変更')).toBeVisible()
  await expect(save).toBeDisabled() // 変更理由が未入力
  await page.getByLabel('変更理由', { exact: true }).fill('E2E: 初回入力')
  await save.click()

  await expect(page.getByText('4 件を保存しました')).toBeVisible()
  // 計算式の科目はサーバーで算出される（52,000.5 × 15 = 780,007.5 → 780,008）
  await expect(page.getByRole('button', { name: `${f.revenueName} 4月: 600,000` })).toBeVisible()
  await expect(page.getByRole('button', { name: `${f.revenueName} 5月: 780,008` })).toBeVisible()
})

test('ロックしたシナリオは参照のみになる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const a = api(page)
  const scenario = await a.post('/scenarios', { name: `E2Eロック ${f.run}`, scenario_kind: 'budget', fiscal_year: 2026 })

  await page.goto(`/scenarios/${scenario.id}`)
  await page.getByRole('button', { name: '🔒 ロックする' }).click()
  await expect(page.getByText('このシナリオはロックされています')).toBeVisible()

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  await expect(page.getByText('このシナリオはロックされているため、参照のみです。')).toBeVisible()
  await expect(page.getByRole('combobox', { name: '科目を追加' })).toHaveCount(0)
})
