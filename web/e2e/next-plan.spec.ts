import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, makeCurrent, seedMasters } from './helpers'

// ほかのテストと年度が重ならないよう、2040・2041年度を使う
test('翌期の計画: 今回の見込と並行して始め、担当者がホームで切り替えて入力できる', async ({ page, browser }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const email = `e2e-plan-${f.run.toLowerCase()}@example.com`
  const member = await a.post('/users', { name: `E2E計画 ${f.run}`, email, role: 'member', password: 'e2e-member-password' })
  const activity = await createActivity(page, f, { owner_user_id: member.id })
  const current = await a.post('/scenarios', { name: `E2E2040見込 ${f.run}`, fiscal_year: 2040, plan_role: 'latest' })
  await makeCurrent(page, current.id)
  await a.put(`/scenarios/${current.id}/activities/${activity.id}/amounts`, { reason: 'E2E', amounts: [{ subject_id: f.revenueId, target_month: '2041-03', amount: 500000 }] })
  // 翌期の計画を空で始める（今回の見込はロックしない）。値は担当者が施策ごとに引き継ぐ（docs/plan.md「2.27」）
  const res = await page.request.post('/api/scenarios/start-fiscal-year', { data: { fiscal_year: 2041, name: `E2E2041計画 ${f.run}`, source: 'empty' } })
  expect(res.status()).toBe(201)

  const ctx = await browser.newContext()
  const mp = await ctx.newPage()
  await login(mp, email, 'e2e-member-password')
  await expect(mp.getByRole('status', { name: '今回の見込' })).toContainText(`E2E2040見込 ${f.run}`)
  await expect(mp.getByRole('status', { name: '翌期の計画' })).toContainText(`E2E2041計画 ${f.run}`)

  // ホーム: 翌期の計画に切り替えると、今回の見込（当期）の年間と比べる
  await mp.getByRole('tab', { name: '翌期の計画' }).click()
  await expect(mp).toHaveURL(/version=plan/)
  await expect(mp.getByRole('columnheader', { name: '今回の見込（当期）との差' })).toBeVisible()
  await mp.getByRole('link', { name: activity.name }).click()

  // 「今回の更新」は翌期の計画で開き、今回の見込（当期）の年間と比べる
  await expect(mp.getByText('翌期の計画（2041年度）を入力しています')).toBeVisible()
  await expect(mp.getByText('今回の見込（当期）との差（年間の利益）')).toBeVisible()

  // 今回の見込から引き継ぐ: 3月の値（500,000）を12か月に、未保存の入力として入れる
  await mp.getByRole('button', { name: '今回の見込から引き継ぐ' }).click()
  await expect(mp.getByText('3月の値を12か月に入れました（まだ保存していません）')).toBeVisible()
  await expect(gridCell(mp, `${f.revenueName} 4月`)).toHaveAccessibleName(`${f.revenueName} 4月: 500,000`)
  await mp.getByLabel('変更理由', { exact: true }).fill('E2E: 今回の見込から引き継ぎ')
  await mp.getByRole('button', { name: '保存', exact: true }).click()
  await expect(mp.getByText('12 件を保存しました')).toBeVisible()
  await ctx.close()
})
