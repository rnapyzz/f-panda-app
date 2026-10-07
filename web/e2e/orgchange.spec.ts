import { expect, test } from '@playwright/test'
import { api, createActivity, login, seedMasters, uniq } from './helpers'

test('組織変更を予約して今すぐ適用し、空いたユニットを廃止し、無効にする人の担当を後任者へ付け替える', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const getJson = async <T,>(path: string) => (await (await page.request.get(`/api${path}`)).json()) as T
  const unitA = (await getJson<{ items: { id: number; name: string; organization_id: number }[] }>('/units')).items.find((u) => u.id === f.unitId)!
  const unitB = await a.post('/units', { name: `E2E移動先 ${f.run}`, segment_id: f.segmentId, organization_id: unitA.organization_id })
  const activity = await createActivity(page, f)

  // 予約を作る（施策を移動先へ）
  await page.goto('/admin/org-changes')
  await page.getByRole('button', { name: '＋ 予約を作成' }).click()
  const dialog = page.getByRole('dialog', { name: '組織変更の予約の作成' })
  const planName = `E2E組織変更 ${f.run}`
  await dialog.getByLabel('名前').fill(planName)
  const form = dialog.getByRole('group', { name: '変更を追加' })
  await form.getByLabel('移動先のユニット').selectOption(String(unitB.id))
  await form.getByLabel('施策を絞り込む').fill(activity.code)
  await form.getByRole('checkbox', { name: new RegExp(activity.name) }).check()
  await form.getByRole('button', { name: '変更を追加' }).click()
  await expect(dialog.getByLabel('変更の一覧')).toContainText(`${activity.name}（${activity.code}）`)
  await dialog.getByRole('button', { name: '保存' }).click()
  await expect(dialog).toHaveCount(0)

  // 作っただけでは移らない。今すぐ適用すると移る
  const row = page.getByRole('row', { name: new RegExp(planName) })
  await expect(row).toContainText('予約中')
  const unitOf = async () => (await getJson<{ unit_id: number }>(`/activities/${activity.id}`)).unit_id
  expect(await unitOf()).toBe(f.unitId)
  await row.getByRole('button', { name: `${planName}を今すぐ適用` }).click()
  await expect(row).toContainText('適用済み')
  expect(await unitOf()).toBe(unitB.id)

  // 空いたユニットを廃止すると、一覧から消える（「廃止したユニットも表示」で見える）
  await page.goto('/masters/units')
  const unitRow = page.getByRole('row', { name: new RegExp(unitA.name) })
  await unitRow.getByRole('button', { name: `${unitA.name}を廃止` }).click()
  await expect(unitRow).toHaveCount(0)
  await page.getByLabel('廃止したユニットも表示').check()
  await expect(page.getByRole('row', { name: new RegExp(unitA.name) })).toContainText('廃止')

  // 担当のある人を無効にするとき、後任者へ付け替える
  const email = `e2e-${uniq().toLowerCase()}@example.com`
  const person = await a.post('/users', { name: `E2E異動者 ${f.run}`, email, role: 'member', password: 'e2e-member-password' })
  const owned = await createActivity(page, f, { code: `ACT2-${f.run}`, name: `E2E担当施策 ${f.run}`, unit_id: unitB.id, owner_user_id: person.id })
  const me = (await getJson<{ user: { id: number; name: string } }>('/auth/me')).user
  await page.goto('/masters/users')
  await page.getByRole('row', { name: new RegExp(`E2E異動者 ${f.run}`) }).getByRole('button', { name: '編集' }).click()
  const userDialog = page.getByRole('dialog', { name: 'ユーザーの編集' })
  await userDialog.getByLabel(/^有効/).uncheck()
  await expect(userDialog.getByLabel('後任者の付け替え')).toContainText('施策 1 件の担当者')
  await userDialog.getByLabel('後任者', { exact: true }).selectOption(String(me.id))
  await userDialog.getByRole('button', { name: '保存' }).click()
  await expect(userDialog).toHaveCount(0)
  expect((await getJson<{ owner_user_id: number }>(`/activities/${owned.id}`)).owner_user_id).toBe(me.id)
})
