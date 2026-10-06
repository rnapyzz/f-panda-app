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
