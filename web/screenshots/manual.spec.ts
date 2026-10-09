import { expect, test, type Page } from '@playwright/test'
import { api, login } from '../e2e/helpers'

// 手引き（src/manual/*.md）に載せる画像を撮る。デモのデータは 2026年度（10月見込を更新中）の想定。
const out = (name: string) => `public/manual/${name}.png`
const memberPassword = 'demo-member-password'
const months = ['2026-04', '2026-05', '2026-06', '2026-07', '2026-08', '2026-09', '2026-10', '2026-11', '2026-12', '2027-01', '2027-02', '2027-03']

test('手引きの画像', async ({ page, browser }) => {
  await login(page)
  const a = api(page)
  const post = <T = { id: number }>(path: string, data: unknown) => a.post<T>(path, data)

  // マスタ
  const seg = await post('/segments', { name: 'SaaS 事業' })
  const seg2 = await post('/segments', { name: '受託開発事業' })
  const org = await post('/organizations', { name: '事業本部' })
  const manager = await post('/users', { name: '佐藤 健', email: 'sato@example.com', role: 'manager', password: memberPassword })
  const suzuki = await post('/users', { name: '鈴木 花子', email: 'suzuki@example.com', role: 'member', password: memberPassword })
  const tanaka = await post('/users', { name: '田中 一郎', email: 'tanaka@example.com', role: 'member', password: memberPassword })
  const saas = await post('/units', { name: 'SaaS ユニット', segment_id: seg.id, organization_id: org.id, owner_user_id: manager.id })
  const dev = await post('/units', { name: '受託開発ユニット', segment_id: seg2.id, organization_id: org.id, owner_user_id: manager.id })
  const subject = async (code: string, name: string, category: string) => {
    const s = await post('/subjects', { code, name, category })
    await post('/gl-accounts', { code, name, subject_id: s.id })
    return s.id
  }
  const subscription = await subject('4110', 'サブスクリプション売上', 'revenue')
  const contract = await subject('4120', '受託売上', 'revenue')
  const outsourcing = await subject('5110', '外注費', 'expense')
  const ads = await subject('5210', '広告宣伝費', 'expense')

  // 施策
  const activity = (code: string, name: string, unit: number, owner: number, type: string, level: string) =>
    post('/activities', { code, name, unit_id: unit, owner_user_id: owner, activity_type: type, status: 'in_progress', confidence_level: level, start_date: '2026-04-01', end_date: '2027-03-31' })
  const plan = await activity('SAAS-001', 'SaaS 月額プラン', saas.id, suzuki.id, 'recurring', 'A')
  const ad = await activity('SAAS-002', '新規獲得キャンペーン', saas.id, tanaka.id, 'project', 'B')
  const bigA = await activity('DEV-001', 'A社 基幹刷新プロジェクト', dev.id, suzuki.id, 'project', 'B')
  const addB = await activity('DEV-002', 'B社 追加開発', dev.id, tanaka.id, 'project', 'C')
  const customers = await post(`/activities/${plan.id}/drivers`, { code: 'customers', name: '契約社数', driver_kind: 'kpi', unit: '社' })
  const price = await post(`/activities/${plan.id}/drivers`, { code: 'unit_price', name: '月額単価', driver_kind: 'value', unit: '円' })
  await post(`/activities/${plan.id}/lines`, { subject_id: subscription, name: '月額利用料', expression: 'unit_price * customers', formula_enabled: true, reason: '算出式の設定' })
  const addon = await post(`/activities/${addB.id}/lines`, { subject_id: contract, name: '追加要件', outlook: 'addon', confidence_level: 'D', reason: '追加要件の見込' })
  await post(`/activities/${bigA.id}/milestones`, { name: '要件定義の完了', due_date: '2026-09-30' })
  await post(`/activities/${bigA.id}/milestones`, { name: '本番リリース', due_date: '2027-02-28' })

  // シナリオ: 期初計画 → 9月見込 → 10月見込（今回）
  const initial = await post('/scenarios', { name: '2026年度 期初計画', fiscal_year: 2026, plan_role: 'initial' })
  const fill = async (sid: number, f: (m: string, i: number) => { aid: number; subject: number; line?: number; amount: number }[]) => {
    const byActivity = new Map<number, unknown[]>()
    months.forEach((m, i) =>
      f(m, i).forEach((x) => byActivity.set(x.aid, [...(byActivity.get(x.aid) ?? []), { subject_id: x.subject, line_id: x.line ?? null, target_month: m, amount: x.amount }])),
    )
    for (const [aid, amounts] of byActivity) await a.put(`/scenarios/${sid}/activities/${aid}/amounts`, { reason: '計画の入力', amounts })
  }
  // from 以降の月（計画値の月）のドライバー値を入れる
  const drivers = async (sid: number, start: number, step: number, from = 0) =>
    a.put(`/scenarios/${sid}/activities/${plan.id}/driver-values`, {
      reason: '計画の入力',
      values: months.slice(from).flatMap((m, j) => [

        { driver_id: customers.id, target_month: m, value: start + step * (from + j) },
        { driver_id: price.id, target_month: m, value: 50000 },
      ]),
    })
  await drivers(initial.id, 120, 5)
  await fill(initial.id, (_m, i) => [
    { aid: ad.id, subject: ads, amount: 1_500_000 },
    { aid: bigA.id, subject: contract, amount: i < 6 ? 4_000_000 : 6_000_000 },
    { aid: bigA.id, subject: outsourcing, amount: i < 6 ? 2_000_000 : 3_000_000 },
    { aid: addB.id, subject: contract, amount: 1_200_000 },
    { aid: addB.id, subject: contract, line: addon.id, amount: i >= 6 ? 800_000 : 0 },
  ])
  const sep = await post('/scenarios', { name: '2026年度 9月見込', fiscal_year: 2026, base_scenario_id: initial.id, actual_through: '2026-08' })
  const oct = await post('/scenarios', { name: '2026年度 10月見込', fiscal_year: 2026, base_scenario_id: sep.id, plan_role: 'latest', actual_through: '2026-09' })

  // 実績（4〜9月）
  const rows = months.slice(0, 6).flatMap((m, i) => [
    `${m},SAAS-001,4110,${(118 + 5 * i) * 50000}`,
    `${m},SAAS-002,5210,${1_400_000 + 50_000 * i}`,
    `${m},DEV-001,4120,${i < 5 ? 4_000_000 : 3_200_000}`,
    `${m},DEV-001,5110,2000000`,
    `${m},DEV-002,4120,1200000`,
  ])
  const res = await page.request.post('/api/actuals/import', {
    multipart: { file: { name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(`target_month,box_code,account_code,amount\n${rows.join('\n')}\n`) }, reason: '2026-09 実績取込' },
  })
  expect(res.ok()).toBeTruthy()
  await a.post(`/scenarios/${oct.id}/activate`, {})
  await a.put(`/scenarios/${oct.id}`, { name: '2026年度 10月見込', plan_role: 'latest', actual_through: '2026-09', previous_scenario_id: sep.id, update_deadline: '2026-10-16', reason: '締切の設定' })

  // 10月見込の更新: A社は後ろ倒し（入力中）、SaaS は完了
  await drivers(oct.id, 120, 6, 6)
  await fill(oct.id, (_m, i) => (i >= 6 ? [{ aid: bigA.id, subject: contract, amount: i < 9 ? 3_000_000 : 8_000_000 }] : []))
  await a.put(`/scenarios/${oct.id}/activities/${plan.id}/note`, { explanation: '9月の新規契約が想定より多く、契約社数を上方修正した。単価は据え置き。', causes: ['volume'] })
  await a.post(`/scenarios/${oct.id}/activities/${plan.id}/complete`, {})

  const shot = async (p: Page, name: string) => {
    await p.waitForLoadState('networkidle')
    await p.screenshot({ path: out(name) })
  }

  // FP&A のホーム（今月の作業）
  await page.goto('/')
  await expect(page.getByRole('list', { name: 'サイクルのチェックリスト' })).toBeVisible()
  await shot(page, 'fpa-work')

  // 施策の一覧（ポートフォリオ・マップ）
  await page.goto('/activities?view=portfolio')
  await expect(page.getByRole('group', { name: /ポートフォリオ・マップ/ })).toBeVisible()
  await page.waitForLoadState('networkidle')
  // 図のカード（凡例と図）だけを撮る
  await page.locator('div:has(> [data-chart])').screenshot({ path: out('activity-portfolio') })

  // リスク画面
  await page.goto('/risks')
  await expect(page.getByRole('img', { name: /悲観/ }).first()).toBeVisible()
  await shot(page, 'risk')

  // 担当者（鈴木）のホームと施策の画面
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: 'ja-JP', timezoneId: 'Asia/Tokyo' })
  const mp = await context.newPage()
  await login(mp, 'suzuki@example.com', memberPassword)
  await expect(mp.getByRole('heading', { name: /更新する施策/ })).toBeVisible()
  await shot(mp, 'home-member')
  await mp.goto(`/activities/${bigA.id}?tab=update&scenario=${oct.id}`)
  await expect(mp.getByRole('table', { name: '目標・前回の見込との比較' })).toBeVisible()
  await shot(mp, 'activity-update')
  const note = mp.locator('section', { has: mp.getByRole('heading', { name: '今回の見込の説明' }) })
  await mp.getByLabel('今回の見込の説明').fill('A社の検収が 12月から 1月にずれたため、Q3 の売上が前回の見込を下回る。年間では目標どおり。')
  await mp.getByText('時期のずれ', { exact: true }).click()
  await note.scrollIntoViewIfNeeded()
  await note.screenshot({ path: out('activity-note') })
  await context.close()
})
