import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('内訳を登録し、計算式の内訳・直接入力の内訳・科目への直接入力を合計する', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  await a.post(`/activities/${activity.id}/drivers`, { code: 'unit_price', name: '月額単価', driver_kind: 'value' })
  await a.post(`/activities/${activity.id}/drivers`, { code: 'customers', name: '契約社数', driver_kind: 'kpi' })
  const scenario = await a.post('/scenarios', { name: `E2E予算 ${f.run}`, scenario_kind: 'budget', fiscal_year: 2026 })

  // 施策の詳細で内訳を追加する（計算式で反映する内訳と、直接入力の内訳）
  await page.goto(`/activities/${activity.id}`)
  const lines = page.locator('section', { has: page.getByRole('heading', { name: '金額の内訳' }) })
  const addLine = async (name: string, expression?: string) => {
    await lines.getByRole('button', { name: '＋ 追加' }).click()
    const dialog = page.getByRole('dialog', { name: '内訳の追加' })
    await dialog.getByLabel('科目').selectOption(String(f.revenueId))
    await dialog.getByLabel('内訳名').fill(name)
    if (expression) {
      await dialog.getByRole('radio', { name: /計算式で反映/ }).check()
      await dialog.getByRole('textbox', { name: /^計算式/ }).fill(expression)
      await dialog.getByLabel('変更理由').fill('E2E: 算出式の設定')
    }
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(lines.getByRole('cell', { name, exact: true })).toBeVisible()
  }
  await addLine('月額利用料', 'unit_price * customers')
  await addLine('初期費用')
  await expect(lines.getByText('計算式で反映')).toBeVisible()

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  // 計算式で反映する内訳は入力できない
  await expect(page.getByRole('button', { name: `${f.revenueName} 月額利用料 4月: 未入力` })).toBeVisible()
  await page.getByLabel('月額単価 4月').fill('50,000')
  await page.getByLabel('契約社数 4月').fill('12')
  await page.getByLabel('月額単価 5月').fill('52000.5')
  await page.getByLabel('契約社数 5月').fill('15')
  await page.getByLabel(`${f.revenueName} 初期費用 4月`).fill('100000')
  await page.getByLabel(`${f.revenueName} その他 4月`).fill('1000')

  const save = page.getByRole('button', { name: '保存', exact: true })
  await expect(page.getByText('6 件の変更')).toBeVisible()
  await expect(save).toBeDisabled() // 変更理由が未入力
  await page.getByLabel('変更理由', { exact: true }).fill('E2E: 初回入力')
  await save.click()

  await expect(page.getByText('6 件を保存しました')).toBeVisible()
  // 計算式の内訳はサーバーで算出される（52,000.5 × 15 = 780,007.5 → 780,008）
  await expect(page.getByRole('button', { name: `${f.revenueName} 月額利用料 4月: 600,000` })).toBeVisible()
  await expect(page.getByRole('button', { name: `${f.revenueName} 月額利用料 5月: 780,008` })).toBeVisible()
  // 科目の合計 = 600,000 + 100,000 + 1,000
  await expect(page.getByRole('row', { name: new RegExp(`^${f.revenueName} .*合計`) }).getByRole('cell').first()).toHaveText('701,000')
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
