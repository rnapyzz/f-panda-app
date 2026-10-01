import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, seedMasters } from './helpers'

test('実績 CSV を確認してから取り込める。取り込んだ実績は、シナリオの決算確定月以前の月に表示される', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)

  // 案件化したので、施策に外部コード（案件番号）を登録する
  const external = `EXT-${f.run}`
  await page.goto(`/activities/${activity.id}`)
  await page.getByLabel('外部コード', { exact: true }).fill(external)
  await page.getByLabel('外部コードのメモ').fill('E2E 案件')
  await page.getByRole('button', { name: '追加', exact: true }).click()
  await expect(page.getByText(external, { exact: true })).toBeVisible()

  await page.goto('/scenarios')
  await page.getByRole('button', { name: '実績を取り込む' }).click()
  const dialog = page.getByRole('dialog', { name: '実績 CSV の取込' })
  const upload = (csv: string) => dialog.getByLabel('CSV ファイル').setInputFiles({ name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  const header = 'target_month,activity_code,subject_code,amount\n'

  // エラーのある CSV
  await upload(`${header}2026-09,${activity.code},R${f.run},1200000\n2026-09,NOPE,R${f.run},1\n`)
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('施策コード "NOPE" は登録されていません')).toBeVisible()
  await expect(dialog.getByRole('cell', { name: '3', exact: true })).toBeVisible() // 行番号

  // 正しい CSV（外部コードと施策コードのどちらでも施策を特定できる）: 確認 → 取込
  await upload(`${header}2026-09,${external},R${f.run},1200000\n2026-09,${activity.code},E${f.run},350000\n`)
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('まだ保存していません')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '取り込む' })).toBeDisabled() // 変更理由が未入力
  await dialog.getByLabel('変更理由').fill('E2E: 9月実績')
  await dialog.getByRole('button', { name: '取り込む' }).click()
  await expect(dialog.getByText('取り込みました')).toBeVisible()
  await dialog.getByRole('button', { name: '閉じる' }).last().click()

  // 決算確定月を 9月にしたシナリオでは、9月以前は実績（入力できない）、10月以降は計画値
  const scenario = await api(page).post('/scenarios', { name: `E2E見込 ${f.run}`, fiscal_year: 2026, actual_through: '2026-09' })
  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  const sep = gridCell(page, `${f.revenueName} 9月`)
  await expect(sep).toHaveAccessibleName(`${f.revenueName} 9月: 1,200,000`)
  await expect(sep).toHaveAttribute('aria-readonly', 'true')
  await expect(gridCell(page, `${f.revenueName} 10月`)).toHaveAttribute('aria-readonly', 'false')
})
