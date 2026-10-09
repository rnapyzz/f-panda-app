import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// 作成中のシナリオを変えないよう、作成中ではない 2033年度のシナリオを使う
test('閲覧制限のある科目は、現場には金額を出さず、変更履歴は施策の画面から開く', async ({ page, browser }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)

  // 勘定科目の画面で、閲覧制限のある科目を作る
  const personnelName = `E2E人件費 ${f.run}`
  await page.goto('/masters/subjects')
  await page.getByRole('button', { name: '＋ 科目を追加' }).click()
  const dialog = page.getByRole('dialog', { name: '科目の追加' })
  await dialog.getByLabel('科目コード').fill(`P${f.run}`)
  await dialog.getByLabel('科目名').fill(personnelName)
  await dialog.getByLabel('区分').selectOption('expense')
  await dialog.getByLabel(/閲覧制限/).check()
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('row', { name: new RegExp(personnelName) })).toContainText('閲覧制限')

  const subjects = (await (await page.request.get('/api/subjects')).json()) as { items: { id: number; code: string }[] }
  const personnelId = subjects.items.find((s) => s.code === `P${f.run}`)!.id
  const email = `e2e-member-${f.run.toLowerCase()}@example.com`
  const member = await a.post('/users', { name: `E2E担当 ${f.run}`, email, role: 'member', password: 'e2e-member-password' })
  const activity = await createActivity(page, f, { owner_user_id: member.id })
  const scenario = await a.post('/scenarios', { name: `E2E閲覧制限 ${f.run}`, fiscal_year: 2033 })
  await a.put(`/scenarios/${scenario.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, target_month: '2033-10', amount: 1000 },
      { subject_id: personnelId, target_month: '2033-10', amount: 300 },
    ],
  })
  const valuesPath = `/scenarios/${scenario.id}/activities/${activity.id}`
  await page.goto(valuesPath)
  await expect(page.getByText(personnelName).first()).toBeVisible()
  await expect(page.getByText('閲覧制限のある科目を除いた金額です')).toHaveCount(0)

  // 現場担当: 人件費は出さず、注記を出す。メニューに変更履歴はなく、施策の画面から開ける
  const context = await browser.newContext()
  const mp = await context.newPage()
  await login(mp, email, 'e2e-member-password')
  // 現場のメニューは「ホーム・施策・予実比較・リスク」と使い方（docs/plan.md「2.18」「2.21」）
  await expect(mp.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link')).toHaveText(['ホーム', '施策', '予実比較', 'リスク', '報告資料', '使い方'])
  await mp.goto(valuesPath)
  await expect(mp.getByText('閲覧制限のある科目を除いた金額です')).toBeVisible()
  await expect(mp.getByText(f.revenueName).first()).toBeVisible()
  await expect(mp.getByText(personnelName)).toHaveCount(0)
  await mp.getByRole('main').getByRole('link', { name: '変更履歴' }).click()
  await expect(mp.getByRole('heading', { name: '変更履歴' })).toBeVisible()
  await expect(mp.getByText('E2E').first()).toBeVisible()
  await context.close()
})
