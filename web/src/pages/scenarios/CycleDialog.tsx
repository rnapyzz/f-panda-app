import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { Scenario } from '../../api/types'
import { Button, Dialog, ErrorMessage, Field, FormError, Input, Select, fieldError } from '../../components/ui'
import { yearMonthLabel } from '../../lib/format'
import { fiscalMonths } from '../../lib/scenario'
import { useApi } from '../../lib/useApi'

type CycleResult = {
  dry_run: boolean
  lock: { id: number; name: string } | null
  base: { id: number; name: string } | null
  role_from: { id: number; name: string } | null
  scenario: Scenario
  warnings: string[]
}

/**
 * シナリオの切り替え（docs/plan.md「2.16」）。
 * monthly: 月次の見込を始める（前の版のロック → 複製 → 決算確定月・最新見込・前回見込・作成中・締切）
 * fiscal-year: 新年度の期初計画を始める（前の版のロック → 空の版を期初計画として作成中に）
 * 「内容を確認」で変わる内容と注意を出し、注意があっても始められる。
 */
export function CycleDialog({
  mode,
  active,
  onClose,
  onDone,
}: {
  mode: 'monthly' | 'fiscal-year'
  active: Scenario | null
  onClose: () => void
  onDone: (s: Scenario) => Promise<void>
}) {
  const monthly = mode === 'monthly'
  const baseYear = active?.fiscal_year ?? new Date().getFullYear()
  const imported = useApi<{ months: string[] }>(monthly ? `/actuals/months?fiscal_year=${baseYear}` : null)
  const last = imported.data?.months[imported.data.months.length - 1]
  const [name, setName] = useState('')
  const [through, setThrough] = useState('')
  const [fiscalYear, setFiscalYear] = useState(String(baseYear + 1))
  const [deadline, setDeadline] = useState('')
  const [preview, setPreview] = useState<CycleResult | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const throughValue = through || last || ''

  const body = (dryRun: boolean) =>
    monthly
      ? { name, actual_through: throughValue, update_deadline: deadline, dry_run: dryRun }
      : { name, fiscal_year: Number(fiscalYear), update_deadline: deadline, dry_run: dryRun }
  const path = monthly ? '/scenarios/start-monthly' : '/scenarios/start-fiscal-year'
  const send = async (dryRun: boolean) => {
    setBusy(true)
    setError(null)
    try {
      const res = await api.post<CycleResult>(path, body(dryRun))
      if (dryRun) setPreview(res)
      else await onDone(res.scenario)
    } catch (err) {
      setError(err)
      setPreview(null)
    } finally {
      setBusy(false)
    }
  }
  const changed = () => setPreview(null)

  return (
    <Dialog
      open
      wide
      title={monthly ? '月次の見込を始める' : '新年度の期初計画を始める'}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>キャンセル</Button>
          <Button disabled={busy || !name.trim()} onClick={() => send(true)}>
            内容を確認
          </Button>
          <Button variant="primary" disabled={busy || !preview} onClick={() => send(false)}>
            {busy ? '処理中…' : '始める'}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-slate-600">
          {monthly
            ? '今の作成中の版をロックして複製し、決算確定月・エイリアス「最新見込」・前回見込・作成中・締切をまとめて設定します。'
            : '今の作成中の版をロックし、新しい年度の空の版を「期初計画」として作り、作成中にします。'}
        </p>
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="sm:col-span-3">
            <Field label="新しい版の名前" required error={fieldError(error, 'name')}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    changed()
                  }}
                  placeholder={monthly ? '例: 2026年10月見込' : `例: ${fiscalYear}年度 期初計画`}
                />
              )}
            </Field>
          </div>
          {monthly ? (
            <Field label="決算確定月" required error={fieldError(error, 'actual_through')} hint="既定は、実績を取り込み済みの最後の月">
              {(p) => (
                <Select
                  {...p}
                  value={throughValue}
                  onChange={(e) => {
                    setThrough(e.target.value)
                    changed()
                  }}
                >
                  <option value="">選択してください</option>
                  {fiscalMonths(baseYear).map((m) => (
                    <option key={m} value={m}>
                      {yearMonthLabel(m)}
                      {imported.data?.months.includes(m) ? '' : '（未取込）'}
                    </option>
                  ))}
                </Select>
              )}
            </Field>
          ) : (
            <Field label="年度" required error={fieldError(error, 'fiscal_year')}>
              {(p) => (
                <Input
                  {...p}
                  type="number"
                  value={fiscalYear}
                  onChange={(e) => {
                    setFiscalYear(e.target.value)
                    changed()
                  }}
                />
              )}
            </Field>
          )}
          <Field label="締切日" error={fieldError(error, 'update_deadline')} hint="入れると担当者に「更新の開始」が届きます">
            {(p) => (
              <Input
                {...p}
                type="date"
                value={deadline}
                onChange={(e) => {
                  setDeadline(e.target.value)
                  changed()
                }}
              />
            )}
          </Field>
        </div>

        {error && !(error instanceof ApiError && Object.keys(error.details).length > 0) ? <ErrorMessage error={error} /> : null}
        <FormError error={error} fields={['name', 'actual_through', 'fiscal_year', 'update_deadline']} />

        {preview && (
          <div className="space-y-3" aria-label="変わる内容">
            <ul className="space-y-1 rounded-md border border-slate-200 p-3 text-sm">
              {preview.lock && <li>🔒 「{preview.lock.name}」をロックします（実績を固定）</li>}
              {preview.base && <li>📄 「{preview.base.name}」を複製します</li>}
              <li>
                ✨ 新しい版「{preview.scenario.name}」（{preview.scenario.fiscal_year}年度
                {preview.scenario.actual_through ? `・実績〜${yearMonthLabel(preview.scenario.actual_through)}` : '・全月計画'}）を作り、作成中にします
              </li>
              <li>
                🏷 エイリアス「{monthly ? '最新見込' : '期初計画'}」を新しい版に{preview.role_from ? `付け替えます（「${preview.role_from.name}」から）` : '付けます'}
              </li>
              {monthly && preview.base && <li>↩ 前回見込は「{preview.base.name}」になります</li>}
              {preview.scenario.update_deadline && <li>⏰ 締切 {preview.scenario.update_deadline}。担当者に「更新の開始」を通知します</li>}
            </ul>
            {preview.warnings.length > 0 ? (
              <ul className="space-y-1 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800" aria-label="注意">
                {preview.warnings.map((w) => (
                  <li key={w}>⚠ {w}</li>
                ))}
                <li className="text-xs text-amber-700">注意があっても始められます。</li>
              </ul>
            ) : (
              <p className="text-sm text-emerald-700">注意はありません。</p>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}
