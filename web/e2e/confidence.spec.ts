import { expect, test } from '@playwright/test'
import { login, uniq } from './helpers'

test('確度の段階をマスタで追加・編集できる', async ({ page }) => {
  await login(page)
  const code = `Z${uniq().slice(-4)}`.replace(/[^A-Z0-9]/g, '').slice(0, 10)

  await page.goto('/activities')
  await page.getByRole('link', { name: '確度の段階' }).click()
  await expect(page.getByRole('heading', { name: '確度の段階' })).toBeVisible()
  // 初期値（A〜E）と判定基準
  await expect(page.getByRole('row', { name: /^B 高 80%/ })).toContainText('内示・口頭合意がある')

  await page.getByRole('button', { name: '＋ 段階を追加' }).click()
  const add = page.getByRole('dialog', { name: '確度の段階の追加' })
  await add.getByLabel('コード').fill(code)
  await add.getByLabel('名前').fill('撤退検討')
  await add.getByLabel('標準の確率（%）').fill('5')
  await add.getByLabel('判定基準').fill('撤退の検討を始めた')
  await add.getByRole('button', { name: '保存' }).click()
  await expect(add).toHaveCount(0)
  const row = page.getByRole('row', { name: new RegExp(`^${code} 撤退検討`) })
  await expect(row).toContainText('5%')

  await row.getByRole('button', { name: '編集' }).click()
  const edit = page.getByRole('dialog', { name: '確度の段階の編集' })
  await expect(edit.getByLabel('コード')).toBeDisabled()
  await edit.getByLabel('標準の確率（%）').fill('10')
  await edit.getByRole('button', { name: '保存' }).click()
  await expect(edit).toHaveCount(0)
  await expect(row).toContainText('10%')

  await row.getByRole('button', { name: '削除' }).click()
  await page.getByRole('dialog', { name: '確度の段階の削除' }).getByRole('button', { name: '削除' }).click()
  await expect(row).toHaveCount(0)
})
