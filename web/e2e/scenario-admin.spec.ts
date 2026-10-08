import { expect, test } from '@playwright/test'
import { createActivity, login, seedMasters } from './helpers'

test('シナリオ管理で、シナリオを作成して作成中にし、決算確定月を設定してロックできる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const name = `E2E見込 ${f.run}`

  // メニューから開く
  await page.goto('/activities')
  await page.getByRole('link', { name: 'シナリオ管理' }).click()
  await expect(page.getByRole('heading', { name: 'シナリオ管理' })).toBeVisible()
  await page.getByRole('combobox', { name: '年度' }).selectOption('2026')

  // エイリアス付きで作成
  await page.getByRole('button', { name: '＋ シナリオを作成' }).click()
  const create = page.getByRole('dialog', { name: 'シナリオの作成' })
  await create.getByLabel('シナリオ名').fill(name)
  await create.getByLabel('年度（4月開始）').fill('2026')
  await create.getByLabel('エイリアス').selectOption('latest')
  await create.getByLabel('決算確定月').selectOption('')
  await create.getByRole('button', { name: '作成' }).click()
  await expect(create).toHaveCount(0)
  const row = page.getByRole('row', { name: new RegExp(name) })
  await expect(row).toContainText('最新見込')

  // 作成中にすると、すべての画面の上部に表示される
  await page.getByRole('button', { name: `${name}を作成中にする` }).click()
  const bar = page.getByRole('status', { name: '今回の見込' })
  await expect(bar).toContainText(`${name}（最新見込）`)
  await expect(row).toContainText('作成中') // シナリオ管理は正式な用語

  // 施策の画面は、編集できる人には今回の見込（作成中のシナリオ）の「今回の更新」を開く
  await page.goto(`/activities/${activity.id}`)
  await expect(page.getByRole('tab', { name: '今回の更新', selected: true })).toBeVisible()
  await expect(page.getByRole('combobox', { name: 'シナリオ' }).locator('option:checked')).toContainText(name)
  await expect(page.getByRole('heading', { name: activity.name })).toBeVisible()
  await expect(page.getByText(name).first()).toBeVisible()

  // 決算確定月の変更は理由が必須。設定すると実績の月は入力できなくなる
  await page.goto('/admin/scenarios')
  await page.getByRole('combobox', { name: '年度' }).selectOption('2026')
  await page.getByRole('button', { name: `${name}の設定` }).click()
  const settings = page.getByRole('dialog', { name: 'シナリオの設定' })
  await settings.getByLabel('決算確定月').selectOption('2026-06')
  await settings.getByRole('button', { name: '保存' }).click()
  await expect(settings.getByText('決算確定月の変更には変更理由の入力が必要です')).toBeVisible()
  await settings.getByLabel('変更理由').fill('E2E: 6月決算確定')
  await settings.getByRole('button', { name: '保存' }).click()
  await expect(settings).toHaveCount(0)
  await expect(row).toContainText('2026年6月')
  await expect(bar).toContainText('6月まで実績')

  // ロックすると作成中は外れる
  await page.getByRole('button', { name: `${name}をロック` }).click()
  await expect(row).toContainText('ロック済み')
  await expect(bar).toContainText('なし')
})
