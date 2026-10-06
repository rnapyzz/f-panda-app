import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// ほかのテストと実績の月が重ならないよう、2031年度を使う
test('締めた後の実績の修正を取込で知らせ、ロック済みのシナリオの実績を最新にでき、年度を締められる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const importCsv = async (csv: string) => {
    const res = await page.request.post('/api/actuals/import', { multipart: { file: { name: 'a.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) }, reason: 'E2E' } })
    expect(res.ok()).toBeTruthy()
  }
  const header = 'target_month,box_code,account_code,amount\n'
  await importCsv(`${header}2031-04,${activity.code},R${f.run},1000\n`)
  const name = `E2E修正 ${f.run}`
  const scenario = await a.post('/scenarios', { name, fiscal_year: 2031, actual_through: '2031-04' })
  await a.post(`/scenarios/${scenario.id}/lock`, {})

  // 4月の修正を取り込むと、確認画面にロック済みのシナリオとの食い違いが出る
  await page.goto('/admin/scenarios')
  await page.getByRole('button', { name: '実績を取り込む' }).click()
  const upload = page.getByRole('dialog', { name: '実績 CSV の取込' })
  await upload.getByLabel('CSV ファイル').setInputFiles({ name: 'a.csv', mimeType: 'text/csv', buffer: Buffer.from(`${header}2031-04,${activity.code},R${f.run},1250\n`) })
  await upload.getByRole('button', { name: '内容を確認' }).click()
  const drift = upload.getByLabel('ロック済みのシナリオとの食い違い')
  await expect(drift).toContainText(name)
  await expect(drift.getByRole('row', { name: /2031年4月/ })).toContainText('+250')
  await upload.getByLabel('変更理由').fill('E2E: 4月の修正')
  await upload.getByRole('button', { name: '取り込む' }).click()
  await expect(upload.getByText('取り込みました')).toBeVisible()
  await upload.getByRole('button', { name: '閉じる' }).last().click()

  // 一覧に「実績の修正あり」。実績を最新にすると消える（ロックは外れない）
  await page.getByRole('combobox', { name: '年度' }).selectOption('2031')
  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toContainText('実績の修正あり（1か月）')
  await row.getByRole('button', { name: `${name}の実績を最新にする` }).click()
  const refresh = page.getByRole('dialog', { name: '実績を最新にする' })
  await expect(refresh.getByRole('row', { name: /2031年4月/ })).toContainText('+250')
  await refresh.getByLabel('変更理由').fill('E2E: 決算整理を反映')
  await refresh.getByRole('button', { name: '最新にする' }).click()
  await expect(refresh.getByText('実績を最新にしました')).toBeVisible()
  await refresh.getByRole('button', { name: '閉じる' }).last().click()
  await expect(row).not.toContainText('実績の修正あり')
  await expect(row).toContainText('ロック済み')

  // 年度を締めると、その年度の実績は取り込めない。締めを解除する
  const closing = page.getByLabel('年度の締め')
  await closing.getByRole('button', { name: '2031年度を締める' }).click()
  await page.getByRole('dialog', { name: '2031年度を締める理由' }).getByLabel('変更理由').fill('E2E: 決算確定')
  await page.getByRole('dialog', { name: '2031年度を締める理由' }).getByRole('button', { name: '保存' }).click()
  await expect(closing).toContainText('2031年度は締め済み')
  const res = await page.request.post('/api/actuals/import', { multipart: { file: { name: 'a.csv', mimeType: 'text/csv', buffer: Buffer.from(`${header}2031-04,${activity.code},R${f.run},1\n`) }, reason: 'E2E' } })
  expect(res.status()).toBe(422)
  await closing.getByRole('button', { name: '締めを解除' }).click()
  await page.getByRole('dialog', { name: '2031年度の締めを解除する理由' }).getByLabel('変更理由').fill('E2E: 後片付け')
  await page.getByRole('dialog', { name: '2031年度の締めを解除する理由' }).getByRole('button', { name: '保存' }).click()
  await expect(closing.getByRole('button', { name: '2031年度を締める' })).toBeVisible()
})
