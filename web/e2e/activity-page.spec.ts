import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, seedMasters } from './helpers'

// 作成中のシナリオを変えないよう、作成中ではない 2035年度のシナリオを FP&A が使う
test('施策の画面: 「今回の更新」の表から内訳を追加でき、タブを切り替えても未保存の入力が残る。遅れたマイルストーンを知らせる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const scenario = await a.post('/scenarios', { name: `E2E施策画面 ${f.run}`, fiscal_year: 2035 })
  await a.post(`/activities/${activity.id}/milestones`, { name: '検収', due_date: '2020-01-31' })

  await page.goto(`/activities/${activity.id}?tab=update&scenario=${scenario.id}`)
  await expect(page.getByRole('tab', { name: '今回の更新', selected: true })).toBeVisible()
  const notice = page.getByRole('status', { name: 'マイルストーンの知らせ' })
  await expect(notice).toContainText('検収')

  // 表から内訳を追加する
  await page.getByRole('button', { name: '＋ 内訳を追加' }).click()
  const dialog = page.getByRole('dialog', { name: '内訳の追加' })
  await dialog.getByLabel('科目').selectOption(String(f.revenueId))
  await dialog.getByLabel('内訳名').fill('保守')
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toHaveCount(0)
  const cell = gridCell(page, `${f.revenueName} 保守 4月`)
  await expect(cell).toBeVisible()

  // 未保存の入力は、ほかのタブに切り替えても残る
  await cell.click()
  await page.keyboard.type('1000')
  await page.keyboard.press('Enter')
  await page.getByRole('tab', { name: '設定' }).click()
  await expect(page.getByRole('tabpanel', { name: '設定' }).getByRole('cell', { name: '保守', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: /今回の更新/ }).click()
  await expect(gridCell(page, `${f.revenueName} 保守 4月`)).toHaveAccessibleName(/1,000/)
  await expect(page.getByText('1 件の変更')).toBeVisible()

  // 知らせから「概要」のマイルストーンへ
  await notice.getByRole('button', { name: '概要で更新する →' }).click()
  await expect(page.getByRole('tab', { name: '概要', selected: true })).toBeVisible()
  await expect(page.getByRole('tabpanel', { name: '概要' }).getByRole('cell', { name: '検収' })).toBeVisible()
})
