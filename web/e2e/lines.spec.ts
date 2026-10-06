import { expect, test } from '@playwright/test'
import { createActivity, login, seedMasters } from './helpers'

test('内訳に見通しの種類と確度の段階を設定でき、変更には理由が求められる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f) // 運用型なので施策の段階は A

  await page.goto(`/activities/${activity.id}`)
  const lines = page.locator('section', { has: page.getByRole('heading', { name: '金額の内訳' }) })
  await lines.getByRole('button', { name: '＋ 追加' }).click()
  const add = page.getByRole('dialog', { name: '内訳の追加' })
  await add.getByLabel('科目').selectOption(String(f.revenueId))
  await add.getByLabel('内訳名').fill('解約リスク')
  await add.getByRole('radio', { name: /ダウンサイド/ }).check()
  // 既定は施策と同じ段階
  await expect(add.getByLabel('確度の段階')).toHaveValue('')
  await expect(add.getByLabel('確度の段階').locator('option').first()).toHaveText('施策と同じ（A 確定（100%））')
  await add.getByLabel('確度の段階').selectOption('D')
  await expect(add.getByText('判定基準: 引き合い・初回提案の段階')).toBeVisible()
  await add.getByRole('button', { name: '保存' }).click()
  await expect(add).toHaveCount(0)

  const row = lines.getByRole('row', { name: /解約リスク/ })
  await expect(row).toContainText('ダウンサイド')
  await expect(row).toContainText('D 低（20%）')

  // 見通しの種類を変えるときは理由が必須
  await row.getByRole('button', { name: '編集' }).click()
  const edit = page.getByRole('dialog', { name: '内訳の編集' })
  await expect(edit.getByLabel('変更理由')).toHaveCount(0)
  await edit.getByRole('radio', { name: /アドオン/ }).check()
  await edit.getByLabel('変更理由').fill('E2E: 追加要件として見直し')
  await edit.getByRole('button', { name: '保存' }).click()
  await expect(edit).toHaveCount(0)
  await expect(row).toContainText('アドオン')
})
