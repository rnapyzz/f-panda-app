import { expect, test } from '@playwright/test'
import { adminEmail, login } from './helpers'

test('パスワードを間違えるとエラーが表示される', async ({ page }) => {
  await page.goto('/')
  await page.getByLabel('メールアドレス').fill(adminEmail)
  await page.getByLabel('パスワード').fill('wrong-password-123')
  await page.getByRole('button', { name: 'ログイン' }).click()
  await expect(page.getByRole('alert')).toHaveText('メールアドレスまたはパスワードが正しくありません')
})

test('ログインすると開いていた画面が表示され、ログアウトでログイン画面に戻る', async ({ page }) => {
  await page.goto('/masters/subjects')
  await expect(page.getByRole('button', { name: 'ログイン' })).toBeVisible()
  await login(page)
  // ログイン画面に移動していないので、ディープリンクの画面がそのまま表示される
  await page.goto('/masters/subjects')
  await expect(page.getByRole('heading', { name: '勘定科目' })).toBeVisible()

  await page.getByRole('button', { name: 'ログアウト' }).click()
  await expect(page.getByRole('button', { name: 'ログイン' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button', { name: 'ログイン' })).toBeVisible()
})
