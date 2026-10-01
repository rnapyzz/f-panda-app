import { expect, test } from '@playwright/test'
import { api, createActivity, gridCell, login, seedMasters } from './helpers'

test('内訳を登録し、計算式の内訳・直接入力の内訳・科目への直接入力を合計する', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const a = api(page)
  const activity = await createActivity(page, f)
  await a.post(`/activities/${activity.id}/drivers`, { code: 'unit_price', name: '月額単価', driver_kind: 'value' })
  await a.post(`/activities/${activity.id}/drivers`, { code: 'customers', name: '契約社数', driver_kind: 'kpi' })
  const scenario = await a.post('/scenarios', { name: `E2E予算 ${f.run}`, fiscal_year: 2026 })

  // 施策の詳細で内訳を追加する（計算式で反映する内訳と、直接入力の内訳）
  await page.goto(`/activities/${activity.id}`)
  const lines = page.locator('section', { has: page.getByRole('heading', { name: '金額の内訳' }) })
  const addLine = async (name: string, expression?: string) => {
    await lines.getByRole('button', { name: '＋ 追加' }).click()
    const dialog = page.getByRole('dialog', { name: '内訳の追加' })
    await dialog.getByLabel('科目').selectOption(String(f.revenueId))
    await dialog.getByLabel('内訳名').fill(name)
    if (expression) {
      await dialog.getByRole('radio', { name: /計算式で反映/ }).check()
      await dialog.getByRole('textbox', { name: /^計算式/ }).fill(expression)
      await dialog.getByLabel('変更理由').fill('E2E: 算出式の設定')
    }
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(lines.getByRole('cell', { name, exact: true })).toBeVisible()
  }
  await addLine('月額利用料', 'unit_price * customers')
  await addLine('初期費用')
  await expect(lines.getByText('計算式で反映')).toBeVisible()

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  // 計算式で反映する内訳は入力できない
  const fx = gridCell(page, `${f.revenueName} 月額利用料 4月`)
  await expect(fx).toHaveAttribute('aria-readonly', 'true')
  await fx.click()
  await page.keyboard.type('999')
  await expect(fx).toHaveAccessibleName(`${f.revenueName} 月額利用料 4月: 未入力`)

  // スプレッドシートのように、選んで入力し Tab で右へ、Enter で確定する
  await gridCell(page, '月額単価 4月').click()
  await page.keyboard.type('50,000')
  await page.keyboard.press('Tab')
  await page.keyboard.type('52000.5')
  await page.keyboard.press('Enter')
  await gridCell(page, '契約社数 4月').click()
  await page.keyboard.type('12')
  await page.keyboard.press('Tab')
  await page.keyboard.type('15')
  await page.keyboard.press('Enter')
  await gridCell(page, `${f.revenueName} 初期費用 4月`).click()
  await page.keyboard.type('100000')
  await page.keyboard.press('Enter') // 下の「その他」へ移る
  await page.keyboard.type('1000')
  await page.keyboard.press('Enter')

  const save = page.getByRole('button', { name: '保存', exact: true })
  await expect(page.getByText('6 件の変更')).toBeVisible()
  await expect(save).toBeDisabled() // 変更理由が未入力
  await page.getByLabel('変更理由', { exact: true }).fill('E2E: 初回入力')
  await save.click()

  await expect(page.getByText('6 件を保存しました')).toBeVisible()
  // 計算式の内訳はサーバーで算出される（52,000.5 × 15 = 780,007.5 → 780,008）
  await expect(gridCell(page, `${f.revenueName} 月額利用料 4月`)).toHaveAccessibleName(`${f.revenueName} 月額利用料 4月: 600,000`)
  await expect(gridCell(page, `${f.revenueName} 月額利用料 5月`)).toHaveAccessibleName(`${f.revenueName} 月額利用料 5月: 780,008`)
  // 科目の合計 = 600,000 + 100,000 + 1,000
  await expect(page.getByRole('row', { name: new RegExp(`^${f.revenueName} .*合計`) }).locator('td').first()).toHaveText('701,000')
})

test('範囲の選択・右方向へのコピー・貼り付け・消去・元に戻す', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const scenario = await api(page).post('/scenarios', { name: `E2Eグリッド ${f.run}`, fiscal_year: 2026 })
  const rev = f.revenueName

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  await page.getByRole('combobox', { name: '科目を追加' }).selectOption(String(f.revenueId))

  // 4月に入力し、4月〜3月を選んで Ctrl/⌘+R で右方向にコピー
  await gridCell(page, `${rev} 4月`).click()
  await page.keyboard.type('1000')
  await page.keyboard.press('Enter')
  await gridCell(page, `${rev} 4月`).click()
  await gridCell(page, `${rev} 3月`).click({ modifiers: ['Shift'] })
  await page.keyboard.press('ControlOrMeta+r')
  await expect(gridCell(page, `${rev} 3月`)).toHaveAccessibleName(`${rev} 3月: 1,000`)
  await expect(page.getByText('12 件の変更')).toBeVisible()

  // 元に戻す・やり直す
  await page.keyboard.press('ControlOrMeta+z')
  await expect(gridCell(page, `${rev} 3月`)).toHaveAccessibleName(`${rev} 3月: 未入力`)
  await page.keyboard.press('ControlOrMeta+Shift+z')
  await expect(gridCell(page, `${rev} 3月`)).toHaveAccessibleName(`${rev} 3月: 1,000`)

  // Excel からの貼り付け（タブ区切り・桁区切り・△のマイナス）
  await gridCell(page, `${rev} 5月`).click()
  await page.evaluate(() => {
    const data = new DataTransfer()
    data.setData('text/plain', '2,000\t△500\r\n')
    document.activeElement!.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
  })
  await expect(gridCell(page, `${rev} 5月`)).toHaveAccessibleName(`${rev} 5月: 2,000`)
  await expect(gridCell(page, `${rev} 6月`)).toHaveAccessibleName(`${rev} 6月: -500`)

  // Delete で消去
  await gridCell(page, `${rev} 7月`).click()
  await page.keyboard.press('Delete')
  await expect(gridCell(page, `${rev} 7月`)).toHaveAccessibleName(`${rev} 7月: 未入力`)

  // Esc で編集を取り消す
  await gridCell(page, `${rev} 8月`).click()
  await page.keyboard.type('77')
  await page.keyboard.press('Escape')
  await expect(gridCell(page, `${rev} 8月`)).toHaveAccessibleName(`${rev} 8月: 1,000`)

  await page.getByLabel('変更理由', { exact: true }).fill('E2E: グリッド操作')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('11 件を保存しました')).toBeVisible()
  // 1,000 × 9 か月 + 2,000 − 500
  await expect(page.getByRole('row', { name: new RegExp(`^${rev}`) }).locator('td').last()).toHaveText('10,500')
})

test('ロックしたシナリオは参照のみになる', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const a = api(page)
  const scenario = await a.post('/scenarios', { name: `E2Eロック ${f.run}`, fiscal_year: 2026 })

  await page.goto(`/scenarios/${scenario.id}`)
  await page.getByRole('button', { name: '🔒 ロックする' }).click()
  await expect(page.getByText('このシナリオはロックされています')).toBeVisible()

  await page.goto(`/scenarios/${scenario.id}/activities/${activity.id}`)
  await expect(page.getByText('このシナリオはロックされているため、参照のみです。')).toBeVisible()
  await expect(page.getByRole('combobox', { name: '科目を追加' })).toHaveCount(0)
})
