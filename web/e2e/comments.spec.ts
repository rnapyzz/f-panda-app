import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

test('説明にコメントを書くと担当者にお知らせが届き、返信や削除ができる', async ({ page, browser }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const email = `e2e-comment-${f.run.toLowerCase()}@example.com`
  const member = await a.post('/users', { name: `E2E担当 ${f.run}`, email, role: 'member', password: 'e2e-member-password' })
  const activity = await createActivity(page, f, { owner_user_id: member.id })
  const scenario = await a.post('/scenarios', { name: `E2Eコメント ${f.run}`, fiscal_year: 2026 })

  // FP&A がコメントを書く
  await page.goto(`/activities/${activity.id}?tab=update&scenario=${scenario.id}`)
  const thread = page.getByRole('region', { name: 'コメント' })
  await expect(thread).toContainText('コメントはまだありません')
  await thread.getByLabel('コメントを書く').fill('受注の時期の根拠を教えてください')
  await thread.getByRole('button', { name: 'コメントする' }).click()
  await expect(thread.getByText('受注の時期の根拠を教えてください')).toBeVisible()
  await expect(thread.getByLabel('コメントを書く')).toHaveValue('')

  // 担当者: お知らせから施策を開いて返信する
  const ctx = await browser.newContext()
  const mp = await ctx.newPage()
  await login(mp, email, 'e2e-member-password')
  await mp.getByRole('button', { name: /お知らせ/ }).click()
  await mp.getByText(`「E2E施策 ${f.run}」の説明にE2E 管理者さんがコメントしました`).click()
  await expect(mp).toHaveURL(new RegExp(`/activities/${activity.id}\\?tab=update&scenario=${scenario.id}`))
  const mthread = mp.getByRole('region', { name: 'コメント' })
  await expect(mthread).toContainText('受注の時期の根拠を教えてください')
  await expect(mthread.getByRole('button', { name: '削除' })).toHaveCount(0) // 他人のコメントは消せない
  await mthread.getByLabel('コメントを書く').fill('先方の稟議が11月のためです')
  await mthread.getByRole('button', { name: 'コメントする' }).click()
  await expect(mthread.getByText('先方の稟議が11月のためです')).toBeVisible()
  await ctx.close()

  // FP&A: 自分のコメントを消すと「削除されました」と表示され、返信は残る
  await page.reload()
  await thread.getByRole('button', { name: '削除' }).click()
  await expect(thread.getByText('このコメントは削除されました')).toBeVisible()
  await expect(thread.getByText('先方の稟議が11月のためです')).toBeVisible()
  await expect(thread.getByRole('heading', { name: 'コメント（1）' })).toBeVisible()
})
