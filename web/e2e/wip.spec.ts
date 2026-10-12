import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, makeCurrent, seedMasters } from './helpers'

type Subject = { id: number; code: string; name: string; role: string }

// ほかのテストと年度が重ならないよう、2044年度を使う
test('検収月に一括の施策は、計上月から仕掛品振替を計算し、原価と仕掛を概要に出す', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)

  // 仕掛品振替の科目は全体で1つ（ほかの実行で作ってあれば使う）
  const subjects = (await (await page.request.get('/api/subjects')).json()) as { items: Subject[] }
  const wip = subjects.items.find((s) => s.role === 'wip_transfer') ?? (await a.post<Subject>('/subjects', { code: `WIP${f.run}`, name: `E2E仕掛品振替 ${f.run}`, category: 'expense', role: 'wip_transfer' }))

  // 画面で、外注費を原価の科目にする
  await page.goto('/masters/subjects')
  await page.getByRole('row', { name: new RegExp(`E2E外注費 ${f.run}`) }).getByRole('button', { name: '編集' }).click()
  await page.getByLabel('プロジェクトの原価と仕掛').selectOption({ label: '原価の科目（仕掛品に積む）' })
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByRole('row', { name: new RegExp(`E2E外注費 ${f.run}`) })).toContainText('原価')

  // 4月着手・9月検収: 4〜10月に外注費 150 万、9月に売上 1,200 万
  const activity = await createActivity(page, f, { activity_type: 'project', start_date: '2044-04-01', end_date: '2044-10-31', recognition: 'on_delivery' })
  const scenario = await a.post('/scenarios', { name: `E2E2044見込 ${f.run}`, fiscal_year: 2044, plan_role: 'latest' })
  await makeCurrent(page, scenario.id)
  const months = ['2044-04', '2044-05', '2044-06', '2044-07', '2044-08', '2044-09', '2044-10']
  await a.put(`/scenarios/${scenario.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [...months.map((m) => ({ subject_id: f.expenseId, target_month: m, amount: 1500000 })), { subject_id: f.revenueId, target_month: '2044-09', amount: 12000000 }],
  })

  // 今回の更新: 計上月を入れるまでは知らせが出る。9月にすると振替を計算する
  await page.goto(`/activities/${activity.id}?tab=update&scenario=${scenario.id}`)
  await expect(page.getByText('計上月（検収月）を入れてください')).toBeVisible()
  await page.getByLabel('計上月', { exact: true }).selectOption({ label: '2044年9月' })
  await expect(page.getByText('計上月（検収月）を入れてください')).toBeHidden()
  await expect(gridCell(page, `${wip.name} 4月`)).toHaveAccessibleName(`${wip.name} 4月: ▲1,500,000`)
  await expect(gridCell(page, `${wip.name} 9月`)).toHaveAccessibleName(`${wip.name} 9月: 7,500,000`)

  // 概要: 原価と仕掛（8月末の仕掛残高 750 万、9月の売上原価 900 万）
  await page.getByRole('tab', { name: '概要' }).click()
  await page.getByLabel('年度', { exact: true }).selectOption('2044')
  await page.getByRole('radiogroup', { name: '金額の単位' }).first().getByRole('radio', { name: '円', exact: true }).click()
  const section = page.getByRole('region', { name: '原価と仕掛' })
  await expect(section).toContainText('計上月 2044年9月')
  await expect(section.getByRole('row', { name: /^月末仕掛残高/ })).toContainText('7,500,000')
  await expect(section.getByRole('row', { name: /^売上原価/ })).toContainText('9,000,000')
})
