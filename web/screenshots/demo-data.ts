import type { Page } from '@playwright/test'
import { api } from '../e2e/helpers'

// 研修・試用（make demo、docs/demo.md）と手引きの画像（make manual-screenshots）で使うデモのデータ。
// 2026年度の想定で、10月見込（今回の見込）を現場が更新している途中の状態を作る。日付は固定（締切 2026-10-16）。
// 4つのユニットに施策 40 件。実績は施策コード・外部コード（案件番号）・割当ルール（会計科目 × 部門）で割り当て、未割当も少し残す。
// 「新規開拓（受託）」は枠の施策（docs/plan.md「2.5」）の例で、内訳に α社・β社・γ社・未確定分を持ち、受注した案件の番号を外部コードに登録している。
// FP&A でログインした page を受け取り、API でデータを入れる。施策が1件でもある環境には入れない（本番などに誤って入れないため）。

/** デモのアカウント（ローカルの研修用の値。本番では使わない） */
export const demoPassword = 'demo-member-password'
export const demoAccounts = [
  { role: 'マネージャー', name: '佐藤 健', email: 'sato@example.com' },
  { role: '担当者', name: '鈴木 花子', email: 'suzuki@example.com' },
  { role: '担当者', name: '田中 一郎', email: 'tanaka@example.com' },
  { role: '閲覧者（経営陣）', name: '山田 誠', email: 'yamada@example.com' },
] as const

const months = ['2026-04', '2026-05', '2026-06', '2026-07', '2026-08', '2026-09', '2026-10', '2026-11', '2026-12', '2027-01', '2027-02', '2027-03']

/** 月の末日（YYYY-MM-DD） */
function lastDay(ym: string): string {
  const [y, m] = ym.split('-').map(Number)
  return `${ym}-${String(new Date(y, m, 0).getDate()).padStart(2, '0')}`
}

/** 画像の撮影で使う ID */
export type DemoData = { activityId: number; scenarioId: number }

export async function seedDemo(page: Page): Promise<DemoData> {
  const existing = await page.request.get('/api/activities')
  if (!existing.ok()) throw new Error(`施策の一覧を読めません: ${existing.status()}`)
  if (((await existing.json()) as { items: unknown[] }).items.length > 0) {
    throw new Error('施策がすでにある環境です。デモのデータは空の環境にだけ入れます')
  }
  const memberPassword = demoPassword
  const a = api(page)
  const post = <T = { id: number }>(path: string, data: unknown) => a.post<T>(path, data)

  // マスタ
  const seg = await post('/segments', { name: 'SaaS 事業' })
  const seg2 = await post('/segments', { name: '受託開発事業' })
  const org = await post('/organizations', { name: '事業本部' })
  const manager = await post('/users', { name: '佐藤 健', email: 'sato@example.com', role: 'manager', password: memberPassword })
  await post('/users', { name: '山田 誠', email: 'yamada@example.com', role: 'viewer', password: memberPassword })
  const suzuki = await post('/users', { name: '鈴木 花子', email: 'suzuki@example.com', role: 'member', password: memberPassword })
  const tanaka = await post('/users', { name: '田中 一郎', email: 'tanaka@example.com', role: 'member', password: memberPassword })
  const saas = await post('/units', { name: 'SaaS ユニット', segment_id: seg.id, organization_id: org.id, owner_user_id: manager.id })
  const dev = await post('/units', { name: '受託開発ユニット', segment_id: seg2.id, organization_id: org.id, owner_user_id: manager.id })
  const glOf = new Map<string, number>()
  const subjectCode = new Map<number, string>()
  const subject = async (code: string, name: string, category: string) => {
    const s = await post('/subjects', { code, name, category })
    const gl = await post('/gl-accounts', { code, name, subject_id: s.id })
    glOf.set(code, gl.id)
    subjectCode.set(s.id, code)
    return s.id
  }
  const subscription = await subject('4110', 'サブスクリプション売上', 'revenue')
  const contract = await subject('4120', '受託売上', 'revenue')
  const outsourcing = await subject('5110', '外注費', 'expense')
  const ads = await subject('5210', '広告宣伝費', 'expense')
  const consulting = await subject('4130', 'コンサルティング売上', 'revenue')
  const rent = await subject('5310', '地代家賃', 'expense')
  const seg3 = await post('/segments', { name: 'コンサルティング事業' })
  const seg4 = await post('/segments', { name: '全社共通' })
  const org2 = await post('/organizations', { name: '管理本部' })
  const consult = await post('/units', { name: 'コンサルティングユニット', segment_id: seg3.id, organization_id: org.id, owner_user_id: manager.id })
  const corp = await post('/units', { name: '管理部門', unit_type: 'corporate', segment_id: seg4.id, organization_id: org2.id, owner_user_id: manager.id })

  // 施策
  const activity = (code: string, name: string, unit: number, owner: number, type: string, level: string, start = '2026-04-01', end = '2027-03-31') =>
    post('/activities', { code, name, unit_id: unit, owner_user_id: owner, activity_type: type, status: 'in_progress', confidence_level: level, start_date: start, end_date: end })
  const plan = await activity('SAAS-001', 'SaaS 月額プラン', saas.id, suzuki.id, 'recurring', 'A')
  const ad = await activity('SAAS-002', '新規獲得キャンペーン', saas.id, tanaka.id, 'project', 'B')
  const bigA = await activity('DEV-001', 'A社 基幹刷新プロジェクト', dev.id, suzuki.id, 'project', 'B')
  const addB = await activity('DEV-002', 'B社 追加開発', dev.id, tanaka.id, 'project', 'C')
  // 図（施策の一覧の「図で見る」）用に、タイプ・確度・期間の違う施策を足す
  const cProposal = await activity('DEV-003', 'C社 新規提案', dev.id, tanaka.id, 'project', 'D', '2026-10-01', '2027-03-31')
  const dMaint = await activity('DEV-004', 'D社 保守契約', dev.id, suzuki.id, 'recurring', 'A')
  const enterprise = await activity('SAAS-003', 'エンタープライズプラン', saas.id, suzuki.id, 'project', 'C', '2026-07-01', '2027-03-31')
  const partner = await activity('SAAS-004', 'パートナー販売', saas.id, tanaka.id, 'project', 'E', '2026-11-01', '2027-03-31')
  const ePoc = await activity('DEV-005', 'E社 PoC', dev.id, tanaka.id, 'project', 'B', '2026-05-01', '2026-09-30')
  const pool = await activity('POOL-001', '開発基盤（共通費）', dev.id, manager.id, 'cost_pool', 'A')
  const churn = await post(`/activities/${enterprise.id}/lines`, { subject_id: subscription, name: '解約リスク', outlook: 'downside', confidence_level: 'D', reason: '解約の見込' })
  await post(`/activities/${cProposal.id}/milestones`, { name: '提案書の提出', due_date: '2026-11-15' })
  await post(`/activities/${enterprise.id}/milestones`, { name: 'β版リリース', due_date: '2026-09-20' })
  await post(`/activities/${enterprise.id}/milestones`, { name: '正式リリース', due_date: '2027-01-15' })
  const customers = await post(`/activities/${plan.id}/drivers`, { code: 'customers', name: '契約社数', driver_kind: 'kpi', unit: '社' })
  const price = await post(`/activities/${plan.id}/drivers`, { code: 'unit_price', name: '月額単価', driver_kind: 'value', unit: '円' })
  await post(`/activities/${plan.id}/lines`, { subject_id: subscription, name: '月額利用料', expression: 'unit_price * customers', formula_enabled: true, reason: '算出式の設定' })
  const addon = await post(`/activities/${addB.id}/lines`, { subject_id: contract, name: '追加要件', outlook: 'addon', confidence_level: 'D', reason: '追加要件の見込' })
  await post(`/activities/${bigA.id}/milestones`, { name: '要件定義の完了', due_date: '2026-09-30' })
  await post(`/activities/${bigA.id}/milestones`, { name: '本番リリース', due_date: '2027-02-28' })

  // 外部コード（会計・基幹システムの案件番号）。実績の明細の箱の ID がこの番号なら、その施策に割り当たる
  const externalCode = (aid: number, code: string, note: string) => post(`/activities/${aid}/external-codes`, { code, note, reason: '案件番号の登録' })
  await externalCode(bigA.id, 'PJ-2026-0101', 'A社 基幹刷新（受注番号）')
  await externalCode(dMaint.id, 'PJ-2025-0077', 'D社 保守（継続契約）')

  // 追加の施策（合わせて 40 件）。amount は計画値の月額、from・to は計画のある月（0 = 4月 … 11 = 3月）
  type Extra = { code: string; name: string; unit: number; owner: number; type: string; level: string; subject: number; amount: number; from?: number; to?: number; ext?: string; status?: string }
  const extras: Extra[] = [
    { code: 'SAAS-005', name: 'チームプラン', unit: saas.id, owner: suzuki.id, type: 'recurring', level: 'A', subject: subscription, amount: 1_200_000 },
    { code: 'SAAS-006', name: '年間契約の更新', unit: saas.id, owner: suzuki.id, type: 'recurring', level: 'A', subject: subscription, amount: 2_000_000 },
    { code: 'SAAS-007', name: 'API 連携オプション', unit: saas.id, owner: tanaka.id, type: 'project', level: 'B', subject: subscription, amount: 600_000, from: 3 },
    { code: 'SAAS-008', name: '海外展開（台湾）', unit: saas.id, owner: tanaka.id, type: 'project', level: 'D', subject: subscription, amount: 900_000, from: 6 },
    { code: 'SAAS-009', name: 'カスタマーサクセス強化', unit: saas.id, owner: suzuki.id, type: 'project', level: 'B', subject: outsourcing, amount: 400_000 },
    { code: 'SAAS-010', name: 'ウェビナー集客', unit: saas.id, owner: tanaka.id, type: 'project', level: 'C', subject: ads, amount: 300_000 },
    { code: 'SAAS-011', name: 'インフラ費（クラウド）', unit: saas.id, owner: manager.id, type: 'cost_pool', level: 'A', subject: outsourcing, amount: 1_100_000 },
    { code: 'SAAS-012', name: '解約防止キャンペーン', unit: saas.id, owner: tanaka.id, type: 'project', level: 'C', subject: ads, amount: 200_000, from: 6 },
    { code: 'DEV-007', name: 'F社 保守契約', unit: dev.id, owner: suzuki.id, type: 'recurring', level: 'A', subject: contract, amount: 700_000, ext: 'PJ-2025-0088' },
    { code: 'DEV-008', name: 'G社 データ移行', unit: dev.id, owner: tanaka.id, type: 'project', level: 'B', subject: contract, amount: 1_800_000, from: 2, to: 7, ext: 'PJ-2026-0112' },
    { code: 'DEV-009', name: 'H社 アプリ開発', unit: dev.id, owner: suzuki.id, type: 'project', level: 'C', subject: contract, amount: 2_200_000, from: 7 },
    { code: 'DEV-010', name: 'I社 運用代行', unit: dev.id, owner: tanaka.id, type: 'recurring', level: 'A', subject: contract, amount: 1_000_000, ext: 'PJ-2024-0045' },
    { code: 'DEV-011', name: 'J社 RFP 対応', unit: dev.id, owner: tanaka.id, type: 'project', level: 'E', subject: contract, amount: 3_000_000, from: 9 },
    { code: 'DEV-012', name: '社内ツール刷新', unit: dev.id, owner: manager.id, type: 'cost_pool', level: 'B', subject: outsourcing, amount: 350_000 },
    { code: 'DEV-013', name: 'K社 追加改修', unit: dev.id, owner: suzuki.id, type: 'project', level: 'A', subject: contract, amount: 1_300_000, from: 3, to: 5, ext: 'PJ-2026-0120', status: 'completed' },
    { code: 'DEV-014', name: 'L社 PoC', unit: dev.id, owner: tanaka.id, type: 'project', level: 'D', subject: contract, amount: 800_000, from: 8, to: 10 },
    { code: 'CON-001', name: 'M社 業務改善コンサル', unit: consult.id, owner: suzuki.id, type: 'project', level: 'B', subject: consulting, amount: 1_500_000, from: 1, ext: 'PJ-2026-0201' },
    { code: 'CON-002', name: 'N社 DX 診断', unit: consult.id, owner: tanaka.id, type: 'project', level: 'A', subject: consulting, amount: 1_200_000, from: 0, to: 2, ext: 'PJ-2026-0202', status: 'completed' },
    { code: 'CON-003', name: '研修サービス', unit: consult.id, owner: suzuki.id, type: 'recurring', level: 'A', subject: consulting, amount: 800_000 },
    { code: 'CON-004', name: 'O社 PMO 支援', unit: consult.id, owner: tanaka.id, type: 'recurring', level: 'B', subject: consulting, amount: 1_600_000, from: 3, ext: 'PJ-2026-0205' },
    { code: 'CON-005', name: 'P社 新規提案', unit: consult.id, owner: suzuki.id, type: 'project', level: 'D', subject: consulting, amount: 1_000_000, from: 8 },
    { code: 'CON-006', name: 'セミナー登壇', unit: consult.id, owner: tanaka.id, type: 'project', level: 'C', subject: consulting, amount: 300_000 },
    { code: 'CON-007', name: 'Q社 戦略策定', unit: consult.id, owner: suzuki.id, type: 'project', level: 'C', subject: consulting, amount: 2_500_000, from: 6, to: 9 },
    { code: 'CON-008', name: 'コンサル人材の採用', unit: consult.id, owner: manager.id, type: 'cost_pool', level: 'B', subject: ads, amount: 250_000 },
    { code: 'CORP-001', name: '本社共通費', unit: corp.id, owner: manager.id, type: 'cost_pool', level: 'A', subject: rent, amount: 1_800_000 },
    { code: 'CORP-002', name: '全社広報', unit: corp.id, owner: manager.id, type: 'cost_pool', level: 'B', subject: ads, amount: 400_000 },
    { code: 'CORP-003', name: '情報システム', unit: corp.id, owner: manager.id, type: 'cost_pool', level: 'A', subject: outsourcing, amount: 600_000 },
    { code: 'CORP-004', name: '採用活動', unit: corp.id, owner: manager.id, type: 'cost_pool', level: 'B', subject: ads, amount: 500_000 },
    { code: 'CORP-005', name: '全社研修', unit: corp.id, owner: manager.id, type: 'cost_pool', level: 'C', subject: outsourcing, amount: 200_000, from: 6 },
  ]
  const extraIds = new Map<string, number>()
  for (const x of extras) {
    const from = x.from ?? 0
    const to = x.to ?? 11
    const created = await activity(x.code, x.name, x.unit, x.owner, x.type, x.level, `${months[from]}-01`, lastDay(months[to]))
    extraIds.set(x.code, created.id)
    if (x.ext) await externalCode(created.id, x.ext, `${x.name}（案件番号）`)
  }
  for (const x of extras.filter((e) => e.status)) await a.put(`/activities/${extraIds.get(x.code)}`, { code: x.code, name: x.name, unit_id: x.unit, owner_user_id: x.owner, activity_type: x.type, status: x.status, confidence_level: x.level, start_date: `${months[x.from ?? 0]}-01`, end_date: lastDay(months[x.to ?? 11]), reason: '完了' })

  // 枠の施策: 新規開拓（受託）。内訳で相手先ごとに分け、確度も内訳ごとに持つ
  const frame = await activity('DEV-006', '新規開拓（受託）', dev.id, tanaka.id, 'project', 'C')
  const frameLine = (name: string, level: string) => post(`/activities/${frame.id}/lines`, { subject_id: contract, name, confidence_level: level, reason: '相手先ごとの内訳' })
  const alpha = await frameLine('α社', 'B')
  const beta = await frameLine('β社', 'C')
  const gamma = await frameLine('γ社', 'D')
  const unknown = await frameLine('未確定分', 'E')
  await externalCode(frame.id, 'PJ-2026-0131', 'α社 受注（新規開拓の枠で受ける）')
  await externalCode(frame.id, 'PJ-2026-0144', 'β社 受注（新規開拓の枠で受ける）')

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
    { aid: cProposal.id, subject: contract, amount: i >= 6 ? 2_500_000 : 0 },
    { aid: cProposal.id, subject: outsourcing, amount: i >= 6 ? 1_200_000 : 0 },
    { aid: dMaint.id, subject: contract, amount: 900_000 },
    { aid: enterprise.id, subject: subscription, amount: i >= 3 ? 1_800_000 : 0 },
    { aid: enterprise.id, subject: subscription, line: churn.id, amount: i >= 6 ? -300_000 : 0 },
    { aid: partner.id, subject: subscription, amount: i >= 7 ? 1_000_000 : 0 },
    { aid: ePoc.id, subject: contract, amount: i >= 1 && i <= 5 ? 600_000 : 0 },
    { aid: pool.id, subject: outsourcing, amount: 500_000 },
    ...extras.filter((x) => i >= (x.from ?? 0) && i <= (x.to ?? 11)).map((x) => ({ aid: extraIds.get(x.code)!, subject: x.subject, amount: x.amount })),
    // 新規開拓は期初では枠だけ（未確定分）。相手先は期中に内訳で分ける
    { aid: frame.id, subject: contract, line: unknown.id, amount: i >= 3 ? 3_000_000 : 0 },
  ])
  const sep = await post('/scenarios', { name: '2026年度 9月見込', fiscal_year: 2026, base_scenario_id: initial.id, actual_through: '2026-08' })
  const oct = await post('/scenarios', { name: '2026年度 10月見込', fiscal_year: 2026, base_scenario_id: sep.id, plan_role: 'latest', actual_through: '2026-09' })

  // 実績（4〜9月）。箱の ID は施策コードか外部コード（案件番号）。部門 D900 の家賃・D210 の広告は割当ルールで施策に入る
  type Row = [month: string, box: string, account: string, amount: number, description: string, department?: string]
  const rows: Row[] = months.slice(0, 6).flatMap((m, i): Row[] => {
    const subscriptionTotal = (118 + 5 * i) * 50000
    // SaaS 月額プランは、請求のバッチごとの明細にする（明細の件数が多い施策の例）
    const batch = Math.floor(subscriptionTotal / 6)
    const batches = Array.from({ length: 6 }, (_, b): Row => [m, 'SAAS-001', '4110', b < 5 ? batch : subscriptionTotal - batch * 5, `月額利用料 請求バッチ ${b + 1}`])
    const extraRows = extras
      .filter((x) => i >= (x.from ?? 0) && i <= (x.to ?? 11) && x.code !== 'CORP-001' && x.code !== 'SAAS-010')
      .map((x, k): Row => {
        // 計画との差を少し付ける（施策と月で決まる -6%〜+6%）
        const factor = 1 + (((k * 7 + i * 3) % 13) - 6) / 100
        return [m, x.ext ?? x.code, String(subjectCode.get(x.subject)), Math.round((x.amount * factor) / 10000) * 10000, `${x.name} ${Number(m.slice(5))}月分`]
      })
    return [
      ...batches,
      [m, 'SAAS-002', '5210', 1_400_000 + 50_000 * i, '新規獲得キャンペーン 広告費'],
      [m, 'PJ-2026-0101', '4120', i < 5 ? 4_000_000 : 3_200_000, 'A社 基幹刷新 出来高'],
      [m, 'PJ-2026-0101', '5110', 2_000_000, 'A社 基幹刷新 協力会社'],
      [m, 'DEV-002', '4120', 1_200_000, 'B社 追加開発'],
      [m, 'PJ-2025-0077', '4120', 900_000, 'D社 保守料'],
      [m, 'POOL-001', '5110', 520_000, '開発基盤 クラウド利用料'],
      ...(i >= 3 ? [[m, 'SAAS-003', '4110', 1_500_000, 'エンタープライズプラン 利用料'] as Row] : []),
      ...(i >= 1 ? [[m, 'DEV-005', '4120', 600_000, 'E社 PoC'] as Row] : []),
      ...extraRows,
      // 割当ルール（会計科目 × 部門）で入る行。箱の ID はない
      [m, '', '5310', 1_800_000, '本社 家賃', 'D900'],
      [m, '', '5210', 280_000 + 10_000 * i, 'ウェビナー 広告出稿', 'D210'],
      // 新規開拓: α社は 7月から、β社は 9月から受注（外部コードで枠の施策に入る）
      ...(i >= 3 ? [[m, 'PJ-2026-0131', '4120', 1_500_000, 'α社 開発受託'] as Row] : []),
      ...(i === 5 ? [[m, 'PJ-2026-0144', '4120', 900_000, 'β社 開発受託 初月'] as Row] : []),
      // 未割当（箱の ID が未登録）: FP&A が未割当の一覧で施策を選ぶ練習用
      ...(i === 5 ? [[m, 'PJ-2026-0150', '4120', 450_000, 'R社 スポット案件（番号が未登録）'] as Row, [m, '', '4130', 120_000, '講演謝礼（施策が不明）'] as Row] : []),
    ]
  })
  await post('/allocation-rules', { gl_account_id: glOf.get('5310'), department_code: 'D900', activity_id: extraIds.get('CORP-001'), reason: '本社の家賃は本社共通費で受ける' })
  await post('/allocation-rules', { gl_account_id: glOf.get('5210'), department_code: 'D210', activity_id: extraIds.get('SAAS-010'), reason: 'マーケティング部の広告はウェビナー集客で受ける' })
  const csvText = rows.map(([m, box, account, amount, description, department]) => [m, box, account, department ?? '', amount, `"${description}"`].join(',')).join('\n')
  const res = await page.request.post('/api/actuals/import', {
    multipart: { file: { name: 'actuals.csv', mimeType: 'text/csv', buffer: Buffer.from(`target_month,box_code,account_code,department_code,amount,description\n${csvText}\n`) }, reason: '2026-09 実績取込' },
  })
  if (!res.ok()) throw new Error(`実績の取込に失敗しました: ${res.status()} ${await res.text()}`)
  await a.post(`/scenarios/${oct.id}/activate`, {})
  await a.put(`/scenarios/${oct.id}`, { name: '2026年度 10月見込', plan_role: 'latest', actual_through: '2026-09', previous_scenario_id: sep.id, update_deadline: '2026-10-16', reason: '締切の設定' })

  // 10月見込の更新: A社は後ろ倒し（入力中）、SaaS は完了
  await drivers(oct.id, 120, 6, 6)
  await fill(oct.id, (_m, i) =>
    i >= 6
      ? [
          { aid: bigA.id, subject: contract, amount: i < 9 ? 3_000_000 : 8_000_000 },
          { aid: cProposal.id, subject: contract, amount: 1_800_000 },
          { aid: enterprise.id, subject: subscription, amount: 2_100_000 },
        ]
      : [],
  )
  await a.put(`/scenarios/${oct.id}/activities/${plan.id}/note`, { explanation: '9月の新規契約が想定より多く、契約社数を上方修正した。単価は据え置き。', causes: ['volume'] })
  await a.post(`/scenarios/${oct.id}/activities/${plan.id}/complete`, {})

  // 追加の施策の10月見込: 新規開拓は相手先ごとの内訳に分ける。いくつかの施策は見込を変えて、説明して完了にしている
  const id = (code: string) => extraIds.get(code)!
  await fill(oct.id, (_m, i) =>
    i >= 6
      ? [
          { aid: frame.id, subject: contract, line: alpha.id, amount: 1_500_000 },
          { aid: frame.id, subject: contract, line: beta.id, amount: 1_000_000 },
          { aid: frame.id, subject: contract, line: gamma.id, amount: i >= 8 ? 1_200_000 : 0 },
          { aid: frame.id, subject: contract, line: unknown.id, amount: 500_000 },
          { aid: id('SAAS-008'), subject: subscription, amount: i >= 9 ? 900_000 : 0 },
          { aid: id('CON-007'), subject: consulting, amount: i <= 9 ? 1_800_000 : 0 },
          { aid: id('DEV-008'), subject: contract, amount: i <= 7 ? 1_500_000 : 0 },
        ]
      : [],
  )
  const done = async (aid: number, explanation: string, causes: string[]) => {
    await a.put(`/scenarios/${oct.id}/activities/${aid}/note`, { explanation, causes })
    await a.post(`/scenarios/${oct.id}/activities/${aid}/complete`, {})
  }
  await done(frame.id, 'α社（7月受注）・β社（9月受注）を内訳に分けた。γ社は 12月開始で提案中。未確定分は 1社分に減らした。', ['new'])
  await done(id('SAAS-005'), '計画どおり。', [])
  await done(id('DEV-007'), '保守料は契約どおり。変更なし。', [])
  await done(id('DEV-010'), '計画どおり。', [])
  await done(id('CON-003'), '受講者数は計画どおり。', [])
  await done(id('CON-007'), '先方の予算縮小で、範囲を絞って 1月までに短縮した。', ['volume'])
  await a.put(`/scenarios/${oct.id}/activities/${id('SAAS-008')}/note`, { explanation: '現地法人の設立が遅れ、開始を 1月に後ろ倒し（記入中）', causes: ['timing'] })
  // 説明へのコメント（docs/plan.md「2.24」）: FP&A が A社の後ろ倒しについて聞いている
  await a.post(`/scenarios/${oct.id}/activities/${bigA.id}/comments`, { body: '10〜12月の売上が後ろ倒しになっています。理由と、年度内に戻る見込みを説明に書いてください。' })

  return { activityId: bigA.id, scenarioId: oct.id }
}
