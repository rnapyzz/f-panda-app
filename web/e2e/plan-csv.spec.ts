import { readFile } from 'node:fs/promises'
import { expect, test, type Page } from '@playwright/test'
import { api, createActivity, login, seedMasters } from './helpers'

async function exportCsv(page: Page, button: string): Promise<string> {
  const download = page.waitForEvent('download')
  await page.getByRole('button', { name: button }).click()
  const file = await download
  return (await readFile(await file.path())).toString('utf8').replace(/^﻿/, '')
}

// 作成中のシナリオを変えないよう、作成中ではない 2032年度のシナリオを FP&A が使う
test('計画値の CSV を出力し、確認してから取り込める', async ({ page }) => {
  await login(page)
  const f = await seedMasters(page)
  const activity = await createActivity(page, f)
  const scenario = await api(page).post('/scenarios', { name: `E2E計画値 ${f.run}`, fiscal_year: 2032 })

  await page.goto(`/scenarios/${scenario.id}`)
  await page.getByRole('combobox', { name: '出力するユニット' }).selectOption({ label: `E2E課 ${f.run}` })
  expect(await exportCsv(page, '金額を出力')).toMatch(/^activity_code,activity_name,subject_code,subject_name,line_name,line_type,2032-04,.*,2033-03\r\n$/)

  // 取込（確認 → 取り込む）
  await page.getByRole('button', { name: 'CSV を取り込む' }).click()
  const dialog = page.getByRole('dialog', { name: '計画値の CSV の取込' })
  const csv = `activity_code,subject_code,line_name,2032-04,2032-05\n${activity.code},R${f.run},,1000,2000\n`
  await dialog.getByLabel('CSV ファイル').setInputFiles({ name: 'amounts.csv', mimeType: 'text/csv', buffer: Buffer.from(csv) })
  await dialog.getByRole('button', { name: '内容を確認' }).click()
  await expect(dialog.getByText('取り込むと、次のように変わります')).toBeVisible()
  await expect(dialog.getByRole('row', { name: new RegExp(activity.code) })).toContainText('2')
  await dialog.getByLabel('変更理由').fill('E2E: 計画値の一括入力')
  await dialog.getByRole('button', { name: '取り込む' }).click()
  await expect(dialog.getByText('取り込みました')).toBeVisible()
  await dialog.getByRole('button', { name: '閉じる' }).last().click()

  // 出力に取り込んだ値が出る
  expect(await exportCsv(page, '金額を出力')).toContain(`${activity.code},${activity.name},R${f.run},${f.revenueName},,manual,1000,2000,`)
})
