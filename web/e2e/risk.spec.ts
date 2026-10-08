import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

// ほかのテストとシナリオの年度が重ならないよう、2034年度を使う
test('リスク画面で、楽観・基準・悲観、見込の構成、施策の内訳、警告を確認できる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  const line = (name: string, level: string, outlook: string) =>
    a.post(`/activities/${activity.id}/lines`, { subject_id: f.revenueId, name, confidence_level: level, outlook, reason: 'E2E' })
  const base = await line('案件', 'C', 'base')
  const addon = await line('追加要件', 'D', 'addon')
  const scenario = await a.post('/scenarios', { name: `E2Eリスク ${f.run}`, fiscal_year: 2034 })
  await a.put(`/scenarios/${scenario.id}/activities/${activity.id}/amounts`, {
    reason: 'E2E',
    amounts: [
      { subject_id: f.revenueId, line_id: base.id, target_month: '2034-10', amount: 10000000 },
      { subject_id: f.revenueId, line_id: addon.id, target_month: '2034-10', amount: 2000000 },
    ],
  })

  await page.goto(`/risks?fy=2034&base=${scenario.id}&cmp=0`)
  // サマリー: 悲観 0 ― 基準 5,400,000 ― 楽観 12,000,000（docs/plan.md「2.8」の例の 1万倍）
  const band = page.getByRole('img', { name: /利益: 悲観/ })
  await expect(band).toHaveAccessibleName(/悲観 0、加重見込 5,400,000、楽観 12,000,000/)
  await expect(page.getByText('振れ幅（楽観 − 悲観）').locator('..')).toContainText('12,000,000')

  // 見込の構成: 凡例と、表で見たときの段階別の売上
  await expect(page.getByLabel('凡例')).toContainText('C 中')
  await page.screenshot({ path: 'test-results/risk-page.png', fullPage: true })
  await page.getByLabel('表で見る').check()
  await expect(page.getByRole('row', { name: /^全体/ })).toContainText('10,000,000')

  // 施策の一覧: 行を開くと内訳の段階・見通しの種類
  await page.getByRole('button', { name: `${activity.name}を開く` }).click()
  await expect(page.getByRole('row', { name: /追加要件/ }).last()).toContainText('アドオン')
  await expect(page.getByRole('row', { name: new RegExp(activity.name) }).first()).toContainText('5,400,000')

  // 比較なしでは、比較に基づく警告は判定しないと案内する
  await expect(page.getByText('比較シナリオを選ぶと判定します')).toBeVisible()
})
