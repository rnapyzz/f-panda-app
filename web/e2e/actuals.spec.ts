import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('実績 CSV を確認してから取り込める。エラーがあれば行番号付きで表示される', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const scenario = await api(page).post('/scenarios', { name: `E2E実績 ${f.run}`, scenario_kind: 'actual', fiscal_year: 2026 })

  await page.goto(`/scenarios/${scenario.id}`)
  await page.getByRole('button', { name: '実績 CSV を取り込む' }).click()
  const dialog = page.getByRole('dialog', { name: '実績 CSV の取込' })
  const upload = (csv: string) => dialog.getByLabel('CSV ファイル').setInputFiles({ name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  const header = 'target_month,activity_code,subject_code,amount\n'

  // エラーのある CSV
  await upload(`${header}2026-09,${activity.code},R${f.run},1200000\n2026-09,NOPE,R${f.run},1\n`)
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('施策コード "NOPE" は登録されていません')).toBeVisible()
  await expect(dialog.getByRole('cell', { name: '3', exact: true })).toBeVisible() // 行番号

  // 正しい CSV: 確認 → 取込
  await upload(`${header}2026-09,${activity.code},R${f.run},1200000\n2026-09,${activity.code},E${f.run},350000\n`)
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('まだ保存していません')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '取り込む' })).toBeDisabled() // 変更理由が未入力
  await dialog.getByLabel('変更理由').fill('E2E: 9月実績')
  await dialog.getByRole('button', { name: '取り込む' }).click()
  await expect(dialog.getByText('取り込みました')).toBeVisible()
  await dialog.getByRole('button', { name: '閉じる' }).last().click()

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  await expect(page.getByRole('button', { name: `${f.revenueName} 9月: 1,200,000` })).toBeVisible()
  await expect(page.getByText('実績シナリオの数値は CSV の取込で登録します')).toBeVisible()
})
