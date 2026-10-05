import { expect, test } from '@playwright/test'
import { createActivity, login, seedMasters } from './helpers'

test('施策を重点施策にし、ウォッチして、一覧で絞り込める', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)

  await page.goto(`/activities/${activity.id}`)
  await page.getByRole('button', { name: '重点施策にする' }).click()
  await expect(page.getByRole('heading', { name: activity.name })).toContainText('重点')
  await page.getByRole('button', { name: `${activity.name}をウォッチする` }).click()
  await expect(page.getByRole('button', { name: `${activity.name}のウォッチを外す` })).toBeVisible()

  // 一覧の絞り込み（重点施策のみ・ウォッチのみ）
  await page.goto(`/activities?q=${activity.code}`)
  await page.getByLabel('重点施策のみ').check()
  await expect(page.getByRole('link', { name: activity.name })).toBeVisible()
  await page.getByLabel('ウォッチのみ').check()
  const row = page.getByRole('row', { name: new RegExp(activity.name) })
  await expect(row).toBeVisible()
  // 一覧からウォッチを外すと、ウォッチのみの一覧から消える
  await row.getByRole('button', { name: `${activity.name}のウォッチを外す` }).click()
  await expect(row).toHaveCount(0)

  // 重点施策から外す
  await page.goto(`/activities/${activity.id}`)
  await page.getByRole('button', { name: '重点施策から外す' }).click()
  await expect(page.getByRole('button', { name: '重点施策にする' })).toBeVisible()
})
