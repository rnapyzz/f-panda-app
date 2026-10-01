import { expect, test } from '@playwright/test'
import { login, seedMasters } from './helpers'

test('施策を作成し、確度の段階を変更すると変更理由を求められ、変更履歴に残る', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const name = `E2E施策 ${f.run}`

  // 作成
  await page.goto('/activities')
  await page.getByRole('button', { name: '＋ 施策を追加' }).click()
  const form = page.getByRole('dialog', { name: '施策の追加' })
  await form.getByLabel('施策名').fill(name)
  // 施策コードは空欄のまま（自動採番）
  await form.getByLabel(/^ユニット/).selectOption({ label: `E2E課 ${f.run}` })
  await form.getByRole('button', { name: '保存' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.getByText(/^ACT-\d{4,}$/)).toBeVisible()
  // 段階を選ばなければ施策タイプの既定（運用型は A）
  await expect(page.getByText('A 確定（100%）')).toBeVisible()

  // 確度の変更 → 変更理由のダイアログ
  await page.getByRole('button', { name: '編集', exact: true }).click()
  const edit = page.getByRole('dialog', { name: '施策の編集' })
  const level = edit.getByLabel('確度の段階')
  await level.selectOption('B')
  await expect(edit.getByText('判定基準: 内示・口頭合意がある')).toBeVisible()
  await edit.getByRole('button', { name: '保存' }).click()

  const reason = page.getByRole('dialog', { name: '施策の変更理由' })
  await expect(reason).toBeVisible()
  await expect(reason.getByRole('button', { name: '保存' })).toBeDisabled()
  await reason.getByLabel('変更理由').fill(`E2E: 受注確度の見直し ${f.run}`)
  await reason.getByRole('button', { name: '保存' }).click()

  await expect(reason).toBeHidden()
  await expect(edit).toBeHidden()
  await expect(page.getByText('B 高（80%）')).toBeVisible()

  // 変更履歴
  // ヘッダーのメニューではなく、施策の画面にある「変更履歴」（この施策で絞り込み済み）
  await page.getByRole('main').getByRole('link', { name: '変更履歴' }).click()
  await expect(page.getByRole('heading', { name: '変更履歴' })).toBeVisible()
  await expect(page).toHaveURL(/activity_id=/)
  await page.getByText(`E2E: 受注確度の見直し ${f.run}`).click()
  const detail = page.getByRole('dialog', { name: '変更の内容' })
  await expect(detail.getByText(`施策 ${name}`)).toBeVisible()
  // 確度の段階: A → B
  const levelRow = detail.getByRole('row', { name: /^確度の段階/ })
  await expect(levelRow).toContainText('A')
  await expect(levelRow).toContainText('B')
})
