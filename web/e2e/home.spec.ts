import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('ホームで、作成中のシナリオの施策の状態と差を確認し、実績のお知らせから施策を開ける', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const amount = (sid: number, month: string, value: number) =>
    a.put(`/scenarios/${sid}/activities/${activity.id}/amounts`, { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: month, amount: value }] })

  // 基準（期初計画）→ 前回見込 → 今回（作成中）。4月の実績が前回見込から大きくずれた
  const budget = await a.post('/scenarios', { name: `E2E期初 ${f.run}`, fiscal_year: 2026, plan_role: 'initial' })
  await amount(budget.id, '2026-04', 1000000)
  const previous = await a.post('/scenarios', { name: `E2E前回 ${f.run}`, fiscal_year: 2026, base_scenario_id: budget.id })
  await amount(previous.id, '2026-04', 1200000)
  const csv = `target_month,activity_code,subject_code,amount\n2026-04,${activity.code},R${f.run},800000\n`
  const res = await page.request.post('/api/actuals/import', { multipart: { file: { name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) }, reason: 'E2E: 4月実績' } })
  expect(res.ok()).toBeTruthy()
  const current = await a.post('/scenarios', { name: `E2E今回 ${f.run}`, fiscal_year: 2026, base_scenario_id: previous.id, actual_through: '2026-04' })
  await a.post(`/scenarios/${current.id}/activate`, {})

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'ホーム' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'メインメニュー' }).getByRole('link', { name: 'ホーム', exact: true })).toHaveAttribute('aria-current', 'page')

  // 実績のお知らせ: 前回見込 1,200,000 → 実績 800,000
  const notice = page.getByRole('status', { name: '実績のお知らせ' })
  await expect(notice).toContainText('4月の実績が反映されました')
  await expect(notice.getByRole('link', { name: activity.name })).toBeVisible()

  // 一覧: 自分のユニットに絞り込み、状態と差を確認する
  const unitFilter = page.getByRole('combobox', { name: 'ユニット' })
  if (await unitFilter.isVisible()) await unitFilter.selectOption({ label: `E2E課 ${f.run}` }) // ユニットが複数あるときだけ表示される
  const row = page.getByRole('row', { name: new RegExp(activity.name) })
  await expect(row).toContainText('未着手')
  await expect(row).toContainText('800,000') // 今回（4月は実績）
  await expect(row).toContainText('-200,000') // 基準 1,000,000 との差
  await expect(row).toContainText('-400,000') // 前回見込 1,200,000 との差

  // 施策を開くと、作成中のシナリオの数値入力画面
  await row.getByRole('link', { name: activity.name }).click()
  await expect(page).toHaveURL(new RegExp(`/scenarios/${current.id}/activities/${activity.id}$`))
})
