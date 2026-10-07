import { expect, test } from '@playwright/test'
import { api, login, seedMasters } from './helpers'

// ほかのテストとシナリオの年度が重ならないよう、2032・2033年度を使う
test('月次の見込を始めると、前の版をロックして複製し、最新見込・前回見込・作成中をまとめて切り替える。新年度の期初計画も始められる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const first = `E2E期初 ${f.run}`
  const s = await a.post('/scenarios', { name: first, fiscal_year: 2032, plan_role: 'latest' })
  await a.post(`/scenarios/${s.id}/activate`, {})

  await page.goto('/admin/scenarios')
  await page.getByRole('button', { name: '月次の見込を始める' }).click()
  const dialog = page.getByRole('dialog', { name: '月次の見込を始める' })
  const name = `E2E4月見込 ${f.run}`
  await dialog.getByLabel('新しい版の名前').fill(name)
  await dialog.getByLabel('決算確定月').selectOption('2032-04')
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  const changes = dialog.getByLabel('変わる内容')
  await expect(changes).toContainText(`「${first}」をロックします`)
  await expect(changes).toContainText(`前回見込は「${first}」になります`)
  await expect(dialog.getByLabel('注意')).toContainText('実績を取り込んでいない月があります: 2032-04')
  await dialog.getByRole('button', { name: '始める' }).click()
  await expect(dialog).toHaveCount(0)

  // 新しい版が作成中・最新見込、前の版はロック済み
  const bar = page.getByRole('status', { name: '作成中のシナリオ' })
  await expect(bar).toContainText(`${name}（最新見込）`)
  await expect(page.getByRole('row', { name: new RegExp(`^${first}`) })).toContainText('ロック済み')

  // 新年度の期初計画を始める
  await page.getByRole('button', { name: '新年度の期初計画を始める' }).click()
  const fy = page.getByRole('dialog', { name: '新年度の期初計画を始める' })
  const next = `E2E2033期初 ${f.run}`
  await fy.getByLabel('新しい版の名前').fill(next)
  await fy.getByLabel('年度').fill('2033')
  await fy.getByRole('button', { name: '内容を確認' }).click()
  await expect(fy.getByLabel('変わる内容')).toContainText(`「${name}」をロックします`)
  await fy.getByRole('button', { name: '始める' }).click()
  await expect(fy).toHaveCount(0)
  await expect(bar).toContainText(`${next}（期初計画）`)
})
