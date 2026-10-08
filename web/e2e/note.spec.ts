import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, seedMasters } from './helpers'

test('今回の見込の説明を書いて「説明して完了」にでき、完了の後に数値を変えると入力中に戻る', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const budget = await a.post('/scenarios', { name: `E2E期初 ${f.run}`, fiscal_year: 2026, plan_role: 'initial' })
  await a.put(`/scenarios/${budget.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [{ subject_id: f.revenueId, target_month: '2026-04', amount: 1000000 }],
  })
  const current = await a.post('/scenarios', { name: `E2E今回 ${f.run}`, fiscal_year: 2026, base_scenario_id: budget.id })

  await page.goto(`/scenarios/${current.id}/activities/${activity.id}`)
  const status = page.getByRole('status', { name: '更新の状態' })
  await expect(status).toContainText('未着手')

  // 数値を変えて保存すると入力中。差異のサマリーに基準との差が出る
  const save = async (value: string, reason: string, checkDisabled = false) => {
    await gridCell(page, `${f.revenueName} 4月`).click()
    await page.keyboard.type(value)
    await page.keyboard.press('Enter')
    if (checkDisabled) await expect(page.getByRole('button', { name: '説明して完了' })).toBeDisabled() // 未保存の変更があるうちは完了にできない
    await page.getByLabel('変更理由', { exact: true }).fill(reason)
    await page.getByRole('button', { name: '保存', exact: true }).click()
    await expect(page.getByText('1 件を保存しました')).toBeVisible()
  }
  await save('1500000', 'E2E: 受注の上積み', true)
  await expect(status).toContainText('入力中')
  await expect(page.getByText('+500,000').first()).toBeVisible()

  // 説明なしで差が大きいと確認が出る
  await page.getByRole('button', { name: '説明して完了' }).click()
  const confirm = page.getByRole('dialog', { name: '説明なしで完了にする' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: 'キャンセル' }).click()

  // 要因と説明を書いて完了にする（説明は完了と一緒に保存される）
  await page.getByText('数量・単価の増減', { exact: true }).click()
  await page.getByLabel('今回の見込の説明').fill('B社の追加発注で 4月の売上が増えた')
  await page.getByRole('button', { name: '説明して完了' }).click()
  await expect(status).toContainText('完了')
  await expect(status).toContainText('E2E 管理者')

  // 開き直しても説明が残っている
  await page.reload()
  await expect(page.getByLabel('今回の見込の説明')).toHaveValue('B社の追加発注で 4月の売上が増えた')
  await expect(status).toContainText('完了')

  // 完了の後に数値を変えると入力中に戻る
  await save('1600000', 'E2E: さらに上積み')
  await expect(status).toContainText('入力中')
})
