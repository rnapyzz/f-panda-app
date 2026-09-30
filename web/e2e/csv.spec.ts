import { expect, test } from '@playwright/test'
import { login, uniq } from './helpers'

test('マスタを CSV でエクスポートし、編集した CSV を確認してから取り込める', async ({ page }) => {
  await login(page)
  const run = uniq()
  await page.goto('/masters/organizations')

  // エクスポート（ダウンロード）
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: 'CSV エクスポート' }).click()
  const file = await download
  expect(file.suggestedFilename()).toMatch(/^organizations-\d{8}\.csv$/)

  // 新しい親と子を同じ CSV で取り込む
  const csv = `code,name,parent_code,sort_order\nHQ-${run},本部 ${run},,0\nDEPT-${run},部 ${run},HQ-${run},0\n`
  await page.getByRole('button', { name: 'CSV インポート' }).click()
  const dialog = page.getByRole('dialog', { name: '組織の CSV インポート' })
  await dialog.getByLabel('CSV ファイル').setInputFiles({ name: 'org.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('まだ保存していません')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '取り込む' })).toBeDisabled()
  await dialog.getByLabel('変更理由').fill('E2E: 組織の一括登録')
  await dialog.getByRole('button', { name: '取り込む' }).click()
  await expect(dialog.getByText('取り込みました')).toBeVisible()
  await dialog.getByRole('button', { name: '閉じる' }).last().click()

  // 一覧に反映されている（配下に表示される）
  await expect(page.getByText(`部 ${run}`, { exact: true })).toBeVisible()
  await expect(page.getByText(`DEPT-${run}`)).toBeVisible()
})

test('CSV で追加したユーザーは「パスワード未設定」と表示される', async ({ page }) => {
  await login(page)
  const email = `csv-${uniq().toLowerCase()}@example.com`
  await page.goto('/masters/users')
  await page.getByRole('button', { name: 'CSV インポート' }).click()
  const dialog = page.getByRole('dialog', { name: 'ユーザーの CSV インポート' })
  await dialog.getByLabel('CSV ファイル').setInputFiles({ name: 'users.csv', mimeType: 'text/csv', buffer: Buffer.from(`email,name,role,is_active\n${email},CSV 太郎,member,true\n`) })
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await dialog.getByLabel('変更理由').fill('E2E: ユーザー追加')
  await dialog.getByRole('button', { name: '取り込む' }).click()
  await expect(dialog.getByText('取り込みました')).toBeVisible()
  await dialog.getByRole('button', { name: '閉じる' }).last().click()
  await expect(page.getByRole('row', { name: new RegExp(email) })).toContainText('パスワード未設定')
})

test('エクスポートに失敗したら理由を表示する', async ({ page }) => {
  await login(page)
  // サーバーがエラーを返した場合を再現する
  await page.route('**/api/subjects/export', (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: { code: 'not_found', message: '勘定科目が見つかりません' } }) }),
  )
  await page.goto('/masters/subjects')
  await page.getByRole('button', { name: 'CSV エクスポート' }).click()
  const dialog = page.getByRole('dialog', { name: 'エクスポートできませんでした' })
  await expect(dialog.getByRole('alert')).toHaveText('勘定科目が見つかりません')
  await expect(dialog.getByText('make up')).toBeVisible()
})
