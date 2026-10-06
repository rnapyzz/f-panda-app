import { expect, test } from '@playwright/test'
import { createActivity, login, seedMasters } from './helpers'

test('会計の明細を取り込み、未割当の一覧で施策を選ぶと、ルールとして残り施策の明細に表示される', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const pool = await createActivity(page, f, { code: `POOL-${f.run}`, name: `E2E共通費 ${f.run}`, activity_type: 'cost_pool' })

  // 会計科目の画面で、給与（明細を FP&A 以外に見せない）を追加する
  await page.goto('/masters/gl-accounts')
  await page.getByRole('button', { name: '＋ 会計科目を追加' }).click()
  const dialog = page.getByRole('dialog', { name: '会計科目の追加' })
  await dialog.getByLabel('コード').fill(`S${f.run}`)
  await dialog.getByLabel('名前').fill(`E2E給与 ${f.run}`)
  await dialog.getByLabel('アプリの科目').selectOption({ label: `E${f.run} E2E外注費 ${f.run}（費用）` })
  await dialog.getByLabel('明細を FP&A 以外に見せない').check()
  await dialog.getByRole('button', { name: '保存' }).click()
  const accountRow = page.getByRole('row', { name: new RegExp(`S${f.run}`) })
  await expect(accountRow).toContainText('FP&A のみ')

  // 7月の明細: 施策コードで当たる行、登録されていない箱の ID、部門だけの給与（どちらも未割当）
  const box = `P-${f.run}`
  const csv =
    'target_month,account_code,department_code,box_code,amount,description\n' +
    `2026-07,R${f.run},D1,${activity.code},100000,既存の受託\n` +
    `2026-07,R${f.run},D1,${box},250000,新規案件\n` +
    `2026-07,S${f.run},D9,,40000,E2E 給与\n`
  await page.goto('/admin/actuals')
  await page.getByRole('button', { name: '実績を取り込む' }).click()
  const upload = page.getByRole('dialog', { name: '実績 CSV の取込' })
  await upload.getByLabel('CSV ファイル').setInputFiles({ name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  await upload.getByRole('button', { name: '内容を確認' }).click()
  await expect(upload.getByLabel('割当の根拠')).toContainText('未割当 2 行')
  await upload.getByLabel('変更理由').fill('E2E: 7月実績')
  await upload.getByRole('button', { name: '取り込む' }).click()
  await expect(upload.getByText('未割当の行があります')).toBeVisible()
  await upload.getByRole('button', { name: '閉じる' }).last().click()

  // 未割当の一覧: 箱の ID を施策に割り当てる（外部コードとして登録される）
  const boxRow = page.getByRole('row', { name: new RegExp(box) })
  await expect(boxRow).toContainText('250,000')
  await boxRow.getByRole('button', { name: '施策を選ぶ' }).click()
  const assign = page.getByRole('dialog', { name: '未割当の実績を施策に割り当てる' })
  await assign.getByLabel('割り当てる施策').selectOption({ label: `${activity.code} ${activity.name}` })
  await assign.getByLabel('変更理由').fill('E2E: 新規案件')
  await assign.getByRole('button', { name: '割り当てる' }).click()
  await expect(boxRow).toHaveCount(0)

  // 部門のまとまりを受け皿の施策に割り当てる（割当ルールが追加される）
  const salaryRow = page.getByRole('row', { name: new RegExp(`S${f.run}`) })
  await salaryRow.getByRole('button', { name: '施策を選ぶ' }).click()
  await assign.getByLabel('割り当てる施策').selectOption({ label: `${pool.code} ${pool.name}` })
  await assign.getByLabel('変更理由').fill('E2E: 給与は共通費')
  await assign.getByRole('button', { name: '割り当てる' }).click()
  await expect(salaryRow).toHaveCount(0)

  await page.goto('/masters/allocation-rules')
  await expect(page.getByRole('row', { name: new RegExp(`S${f.run}`) })).toContainText(pool.name)

  // 施策の明細: 箱の ID の行は「未割当の一覧から選択」、外部コードも登録されている
  await page.goto(`/activities/${activity.id}`)
  await expect(page.getByText(box, { exact: true }).first()).toBeVisible()
  const entries = page.getByRole('table').filter({ hasText: '割当の根拠' })
  await page.getByLabel('実績の月').selectOption('2026-07')
  await expect(entries.getByRole('row', { name: /新規案件/ })).toContainText('未割当の一覧から選択')
  await expect(entries.getByRole('row', { name: /既存の受託/ })).toContainText('施策コード')
  await expect(page.getByText('明細の合計 350,000 円')).toBeVisible()

  // 取り込み直すと、ルールで同じ施策に入る
  const res = await page.request.post('/api/actuals/import?dry_run=true', { multipart: { file: { name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) } } })
  const body = await res.json()
  expect(body.allocation).toMatchObject({ activity_code: 1, external_code: 1, rule: 1, unallocated: 0 })
})
