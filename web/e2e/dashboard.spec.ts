import { expect, test } from '@playwright/test'
import { api, createActivity, login, makeCurrent, seedMasters } from './helpers'

// ほかのテストと年度が重ならないよう、2048年度を使う
test('ダッシュボードで年間の状況を見て、差の図を押してセグメント → ユニット → 施策と掘り下げられる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f, { name: `E2Eダッシュボード施策 ${f.run}` })
  // 目標（期初計画）: 4月の売上 100 万。今回の見込: 4月 120 万・5月 30 万（目標より +50 万）
  const initial = await a.post('/scenarios', { name: `E2E2048期初 ${f.run}`, fiscal_year: 2048, plan_role: 'initial' })
  await a.put(`/scenarios/${initial.id}/activities/${activity.id}/amounts`, { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: '2048-04', amount: 1000000 }] })
  const current = await a.post('/scenarios', { name: `E2E2048見込 ${f.run}`, fiscal_year: 2048, plan_role: 'latest', base_scenario_id: initial.id })
  await makeCurrent(page, current.id)
  await a.put(`/scenarios/${current.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, target_month: '2048-04', amount: 1200000 },
      { subject_id: f.revenueId, target_month: '2048-05', amount: 300000 },
    ],
  })

  // メニューから開く
  await page.goto('/')
  await page.getByRole('link', { name: 'ダッシュボード' }).first().click()
  await expect(page.getByRole('heading', { name: 'ダッシュボード', level: 1 })).toBeVisible()
  await page.goto(`/dashboard?fy=2048&scenario=${current.id}`)
  await page.getByRole('radiogroup', { name: '金額の単位' }).getByRole('radio', { name: '円', exact: true }).click()

  // ① 年間の状況
  const kpi = page.getByRole('region', { name: '年間の状況' })
  await expect(kpi).toContainText('1,500,000')
  await expect(kpi).toContainText('+500,000')

  // ③ どこで差が出ているか: セグメント → ユニット → 施策
  const variance = page.getByRole('region', { name: 'どこで差が出ているか' })
  await variance.getByRole('button', { name: new RegExp(`^E2E事業 ${f.run}: 差 \\+500,000`) }).click()
  await expect(page).toHaveURL(/segment=\d+/)
  const crumbs = page.getByRole('navigation', { name: '掘り下げの位置' })
  await expect(crumbs).toContainText(`E2E事業 ${f.run}`)
  await variance.getByRole('button', { name: new RegExp(`^E2E課 ${f.run}: 差`) }).click()
  await expect(page).toHaveURL(/unit=\d+/)
  await expect(crumbs).toContainText(`E2E課 ${f.run}`)
  await expect(variance.getByRole('link', { name: new RegExp(`^E2Eダッシュボード施策 ${f.run}`) })).toBeVisible()

  // 表で見る
  await variance.getByLabel('表で見る').check()
  await expect(variance.getByRole('row', { name: new RegExp(`E2Eダッシュボード施策 ${f.run}`) })).toContainText('+500,000')

  // ⑥ 施策の表と、パンくずで全社に戻る
  await expect(page.getByRole('region', { name: '施策' }).getByRole('row', { name: new RegExp(`E2Eダッシュボード施策 ${f.run}`) })).toContainText('1,500,000')
  await crumbs.getByRole('button', { name: '全社' }).click()
  await expect(page).not.toHaveURL(/segment=/)
})
