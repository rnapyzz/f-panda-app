import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// ほかのテストと年度が重ならないよう、2050年度を使う（作成中にはしない）
test('シナリオの全体像で、計画の P/L・数字から見えること・構成を見て、区分を押して掘り下げられる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  // 運用型（年度の初めから）と、年度の途中で始まるプロジェクト型
  const recurring = await createActivity(page, f, { code: `RUN-${f.run}`, name: `E2E運用 ${f.run}`, start_date: '2050-04-01' })
  const project = await createActivity(page, f, { code: `PRJ-${f.run}`, name: `E2E案件 ${f.run}`, activity_type: 'project', start_date: '2050-10-01', end_date: '2051-03-31' })
  const scenario = await a.post('/scenarios', { name: `E2E2050計画 ${f.run}`, fiscal_year: 2050 })
  const months = ['2050-04', '2050-05', '2050-06', '2050-07', '2050-08', '2050-09', '2050-10', '2050-11', '2050-12', '2051-01', '2051-02', '2051-03']
  await a.put(`/scenarios/${scenario.id}/activities/${recurring.id}/amounts`, { reason: 'E2E', amounts: months.map((m) => ({ subject_id: f.revenueId, target_month: m, amount: 100000 })) })
  await a.put(`/scenarios/${scenario.id}/activities/${project.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, target_month: '2051-03', amount: 1800000 },
      { subject_id: f.expenseId, target_month: '2051-03', amount: 600000 },
    ],
  })

  // シナリオの画面を開くと、全体像のタブ
  await page.goto(`/scenarios/${scenario.id}`)
  await expect(page.getByRole('tab', { name: '全体像' })).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('radiogroup', { name: '金額の単位' }).getByRole('radio', { name: '円', exact: true }).click()

  // 数字から見えること: 売上 300 万（運用 120 万・案件 180 万）、利益 240 万、下期 80%、年度の途中で始まる施策 60%
  const summary = page.getByRole('region', { name: '数字から見えること' })
  await expect(summary).toContainText('売上 3,000,000・利益 2,400,000（利益率 80%）')
  await expect(summary).toContainText('売上の 60% がプロジェクト型')
  await expect(summary).toContainText('下期に売上の 80%')
  await expect(summary).toContainText('年度の途中で始まる施策の売上は 60%')
  // 業績の幅（楽観はすべて計上）と伸びの候補
  await expect(summary.getByRole('group', { name: '業績の幅' }).getByRole('row', { name: /^楽観/ })).toContainText('3,000,000')
  await expect(summary.getByRole('group', { name: '伸びの候補' })).toContainText(`年度の途中で始まる施策: E2E案件 ${f.run} 1,800,000`)

  // 年間の P/L
  const pl = page.getByRole('region', { name: '年間の P/L' })
  await expect(pl.getByRole('row', { name: /^収益の計/ })).toContainText('3,000,000')
  await expect(pl.getByRole('row', { name: /^利益 / })).toContainText('2,400,000')

  // 何でできているか: セグメント → ユニット → 施策と掘り下げる
  const revenue = page.getByRole('region', { name: '何でできているか（売上）' })
  await revenue.getByRole('button', { name: new RegExp(`^E2E事業 ${f.run}:`) }).click()
  await expect(page).toHaveURL(/tab=overview&segment=\d+/)
  await revenue.getByRole('button', { name: new RegExp(`^E2E課 ${f.run}:`) }).click()
  await expect(page).toHaveURL(/unit=\d+/)
  await expect(page.getByRole('navigation', { name: '掘り下げの位置' })).toContainText(`E2E課 ${f.run}`)
  await expect(revenue.getByRole('link', { name: new RegExp(`^E2E案件 ${f.run}:`) })).toBeVisible()

  // 前提と山場: 売上の大きい順
  await expect(page.getByRole('list', { name: '売上の大きい施策' }).getByRole('listitem').first()).toContainText(`E2E案件 ${f.run}`)

  // 施策のタブは今までどおり
  await page.getByRole('tab', { name: '施策' }).click()
  await expect(page.getByRole('row', { name: new RegExp(`E2E運用 ${f.run}`) })).toBeVisible()
})

test('シナリオの説明は FP&A が書き、だれでも読める。ロックすると直せない', async ({ page, browser }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const email = `e2e-narrative-${f.run.toLowerCase()}@example.com`
  await a.post('/users', { name: `E2E担当 ${f.run}`, email, role: 'member', password: 'e2e-member-password' })
  const scenario = await a.post('/scenarios', { name: `E2E2050説明 ${f.run}`, fiscal_year: 2050 })

  await page.goto(`/scenarios/${scenario.id}`)
  const narrative = page.getByRole('region', { name: 'このシナリオの説明' })
  await narrative.getByRole('button', { name: '説明を書く' }).click()
  // 書いている間は、横に「数字から見えること」を並べる
  await expect(narrative.getByRole('complementary', { name: '書くときの手がかり' }).getByRole('region', { name: '数字から見えること' })).toBeVisible()
  await narrative.getByLabel('計画の骨子').fill('既存の深耕で増収増益を目指す')
  await narrative.getByLabel('主なリスクと機会').fill('大型案件の後ろ倒し')
  await narrative.getByRole('button', { name: '保存' }).click()
  // 書いた欄だけを見出し付きで出す
  await expect(narrative.getByRole('term')).toHaveText(['計画の骨子', '主なリスクと機会'])
  await expect(narrative).toContainText('既存の深耕で増収増益を目指す')
  await expect(narrative.getByRole('button', { name: '編集' })).toBeVisible()

  // 現場は読めるが、編集は出ない
  const context = await browser.newContext()
  const mp = await context.newPage()
  await login(mp, email, 'e2e-member-password')
  await mp.goto(`/scenarios/${scenario.id}`)
  const seen = mp.getByRole('region', { name: 'このシナリオの説明' })
  await expect(seen).toContainText('大型案件の後ろ倒し')
  await expect(seen.getByRole('button')).toHaveCount(0)
  await context.close()

  // ロックすると、FP&A にも編集は出ない
  await a.post(`/scenarios/${scenario.id}/lock`, {})
  await page.reload()
  await expect(narrative).toContainText('既存の深耕で増収増益を目指す')
  await expect(narrative.getByRole('button', { name: '編集' })).toHaveCount(0)
})
