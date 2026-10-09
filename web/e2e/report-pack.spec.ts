import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('報告資料: P/L と変動の説明を Excel に、図を画像に出力できる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f, { name: `E2E報告 ${f.run}` })
  // 変動の説明と図は今回の見込を使うので、2026年度の版を作って今回の見込にする
  const scenario = await a.post('/scenarios', { name: `E2E報告 ${f.run}`, fiscal_year: 2026 })
  await a.post(`/scenarios/${scenario.id}/activate`, {})
  await a.put(`/scenarios/${scenario.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [{ subject_id: f.revenueId, target_month: '2026-10', amount: 1000000 }],
  })

  await page.goto('/')
  await page.getByRole('link', { name: '報告資料' }).click()
  await expect(page.getByRole('heading', { name: '報告資料', level: 1 })).toBeVisible()
  await page.getByLabel('範囲').selectOption({ label: `E2E課 ${f.run}` })
  await page.getByRole('radio', { name: '四半期' }).click()

  const excel = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Excel に出力' }).click()
  const file = await excel
  expect(file.suggestedFilename()).toMatch(/^報告資料_2026_\d{8}\.xlsx$/)
  const xlsx = await readFile((await file.path())!)
  expect(xlsx.subarray(0, 2).toString()).toBe('PK')

  // 図: 範囲の施策で描き、1つずつ画像（PNG）で保存できる
  const portfolio = page.getByRole('region', { name: 'ポートフォリオ' })
  await expect(portfolio.getByRole('link', { name: new RegExp(`^E2E報告 ${f.run}: `) })).toBeVisible()
  const image = page.waitForEvent('download')
  await portfolio.getByRole('button', { name: '画像を保存' }).click()
  const png = await image
  expect(png.suggestedFilename()).toMatch(/^ポートフォリオ_.+\.png$/)
  const bytes = await readFile((await png.path())!)
  expect(bytes.subarray(1, 4).toString()).toBe('PNG')

  // まとめて保存: 3つの図をそれぞれ保存する
  const names: string[] = []
  page.on('download', (d) => names.push(d.suggestedFilename()))
  await page.getByRole('button', { name: '図をまとめて保存' }).click()
  await expect.poll(() => names.length).toBe(3)
  expect(names.map((n) => n.split('_')[0])).toEqual(['ポートフォリオ', '増減の内訳', '確度の推移'])
})
