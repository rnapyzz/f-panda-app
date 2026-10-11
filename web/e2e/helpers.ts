import { expect, type APIRequestContext, type Page } from '@playwright/test'

export const adminEmail = process.env.E2E_EMAIL ?? 'e2e-admin@example.com'
export const adminPassword = process.env.E2E_PASSWORD ?? 'e2e-admin-password'

/** テストごとに重複しない名前を作るための接尾辞 */
export function uniq(): string {
  return `${Date.now().toString(36)}${Math.floor(Math.random() * 1296).toString(36)}`.toUpperCase()
}

export async function login(page: Page, email = adminEmail, password = adminPassword) {
  await page.goto('/')
  await page.getByLabel('メールアドレス').fill(email)
  await page.getByLabel('パスワード').fill(password)
  await page.getByRole('button', { name: 'ログイン' }).click()
  await expect(page.getByRole('button', { name: 'ログアウト' })).toBeVisible()
}

/** ログイン済みのページと同じ Cookie で API を呼ぶ（テストデータの準備用） */
export function api(page: Page) {
  const req: APIRequestContext = page.request
  const call = async <T>(method: 'post' | 'put', path: string, data: unknown): Promise<T> => {
    const res = await req[method](`/api${path}`, { data })
    if (!res.ok()) throw new Error(`${method.toUpperCase()} ${path}: ${res.status()} ${await res.text()}`)
    const text = await res.text()
    return (text ? JSON.parse(text) : undefined) as T
  }
  return {
    post: <T = { id: number }>(path: string, data: unknown) => call<T>('post', path, data),
    put: <T = unknown>(path: string, data: unknown) => call<T>('put', path, data),
  }
}

export type Fixture = {
  run: string
  segmentId: number
  unitId: number
  revenueId: number
  expenseId: number
  revenueName: string
  /** 科目と同じコードの会計科目（実績の取込用） */
  revenueAccountId: number
  expenseAccountId: number
}

/** セグメント・組織・ユニット・科目（収益・費用）と、科目と同じコードの会計科目を作る */
export async function seedMasters(page: Page): Promise<Fixture> {
  const a = api(page)
  const run = uniq()
  const seg = await a.post('/segments', { name: `E2E事業 ${run}` })
  const org = await a.post('/organizations', { name: `E2E部 ${run}` })
  const fn = await a.post('/units', { name: `E2E課 ${run}`, segment_id: seg.id, organization_id: org.id })
  const revenueName = `E2E売上 ${run}`
  const rev = await a.post('/subjects', { code: `R${run}`, name: revenueName, category: 'revenue' })
  const exp = await a.post('/subjects', { code: `E${run}`, name: `E2E外注費 ${run}`, category: 'expense' })
  const revAccount = await a.post('/gl-accounts', { code: `R${run}`, name: revenueName, subject_id: rev.id })
  const expAccount = await a.post('/gl-accounts', { code: `E${run}`, name: `E2E外注費 ${run}`, subject_id: exp.id })
  return { run, segmentId: seg.id, unitId: fn.id, revenueId: rev.id, expenseId: exp.id, revenueName, revenueAccountId: revAccount.id, expenseAccountId: expAccount.id }
}

/** 数値入力グリッドのセル（アクセシブルネームは「ラベル: 表示値」） */
export function gridCell(page: Page, label: string) {
  return page.getByRole('gridcell', { name: new RegExp(`^${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}: `) })
}

export async function createActivity(page: Page, f: Fixture, extra: Record<string, unknown> = {}) {
  return api(page).post<{ id: number; code: string; name: string }>('/activities', {
    unit_id: f.unitId,
    code: `ACT-${f.run}`,
    name: `E2E施策 ${f.run}`,
    activity_type: 'recurring',
    status: 'in_progress',
    ...extra,
  })
}

/** 金額の表示単位（百万円・千円・円）を切り替える。画面を開くと百万円になっている */
export async function showUnit(page: Page, unit: '百万円' | '千円' | '円') {
  await page.getByRole('radiogroup', { name: '金額の単位' }).getByRole('radio', { name: unit, exact: true }).click()
}

/**
 * ほかのテストが残した作成中の版をロックして外す（作成中は年度ごとに1つで、今回の見込は最も新しい年度の見込になるため。
 * docs/plan.md「2.25」）。keep の版はそのまま。E2E 専用の DB なのでロックしてよい
 */
export async function clearActives(page: Page, keep: number[] = []) {
  const res = await page.request.get('/api/scenarios/active')
  if (!res.ok()) throw new Error(`作成中のシナリオを読めません: ${res.status()}`)
  const body = (await res.json()) as { current: { id: number } | null; next_plan: { id: number } | null; others: { id: number }[] }
  const ids = [body.current, body.next_plan, ...body.others].filter((s): s is { id: number } => s !== null).map((s) => s.id)
  for (const id of ids.filter((id) => !keep.includes(id))) {
    const r = await page.request.post(`/api/scenarios/${id}/lock`, { data: { reason: 'E2E: ほかのテストの作成中を外す' } })
    if (!r.ok()) throw new Error(`作成中の版をロックできません: ${r.status()} ${await r.text()}`)
  }
}

/** シナリオを、ただ1つの作成中（今回の見込）にする */
export async function makeCurrent(page: Page, scenarioId: number) {
  await clearActives(page, [scenarioId])
  await api(page).post(`/scenarios/${scenarioId}/activate`, {})
}
