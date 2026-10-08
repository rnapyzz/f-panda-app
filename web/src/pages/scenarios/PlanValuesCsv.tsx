import { useState } from 'react'
import { api, ApiError, download, query } from '../../api/client'
import type { Scenario, Unit } from '../../api/types'
import { Button, Card, Dialog, ErrorMessage, Field, Input, Select, Table, Textarea } from '../../components/ui'
import { Help } from '../../components/Help'

type PlanImportResult = {
  dry_run: boolean
  kind: 'amounts' | 'driver_values'
  rows: number
  inserted: number
  updated: number
  deleted: number
  unchanged: number
  activities: { activity_id: number; code: string; name: string; changed: number }[]
  warnings: { kind: 'actual_month' | 'formula_line'; count: number }[]
}

const kindLabels: Record<PlanImportResult['kind'], string> = { amounts: '金額', driver_values: 'ドライバー値' }

const warningLabels: Record<PlanImportResult['warnings'][number]['kind'], string> = {
  actual_month: '実績の月の値は取り込みませんでした',
  formula_line: '計算式で反映する内訳の値は取り込みませんでした（ドライバー値から計算します）',
}

/** 計画値の CSV（金額・ドライバー値）の出力と取込（docs/plan.md「6.3」） */
export function PlanValuesCsvCard({ scenario, units, canImport, onImported }: { scenario: Scenario; units: Unit[]; canImport: boolean; onImported?: () => void }) {
  const [unitId, setUnitId] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [open, setOpen] = useState(false)

  const exportCsv = async (path: string) => {
    setBusy(true)
    setError(null)
    try {
      await download(`/scenarios/${scenario.id}/${path}/export${query({ unit_id: unitId || undefined })}`)
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card
      title={
        <>
          計画値の CSV
          <Help manual="member#csv">空欄は「変えない」、「-」は「消す」、数値は「その値にする」。実績の月と、計算式で反映する内訳の値は取り込みません。</Help>
        </>
      }
      className="mb-4"
    >
      <div className="space-y-3">
        <p className="text-sm text-slate-600">
          金額（直接入力）とドライバー値を、月を横に並べた CSV で出力・取込できます。Excel で編集して、そのまま取り込めます。
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <Select value={unitId} onChange={(e) => setUnitId(e.target.value)} aria-label="出力するユニット" className="w-48 py-1.5">
            <option value="">全ユニット</option>
            {units.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name}
              </option>
            ))}
          </Select>
          <Button onClick={() => exportCsv('amounts')} disabled={busy}>
            金額を出力
          </Button>
          <Button onClick={() => exportCsv('driver-values')} disabled={busy}>
            ドライバー値を出力
          </Button>
          {canImport && (
            <Button variant="primary" onClick={() => setOpen(true)}>
              CSV を取り込む
            </Button>
          )}
        </div>
        {error ? <ErrorMessage error={error} /> : null}
      </div>
      {open && <PlanValuesImportDialog scenario={scenario} onClose={() => setOpen(false)} onImported={onImported} />}
    </Card>
  )
}

function PlanValuesImportDialog({ scenario, onClose, onImported }: { scenario: Scenario; onClose: () => void; onImported?: () => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<PlanImportResult | null>(null)
  const [done, setDone] = useState<PlanImportResult | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)

  const send = async (dryRun: boolean) => {
    if (!file) return
    setBusy(true)
    setError(null)
    const form = new FormData()
    form.set('file', file)
    form.set('reason', reason)
    try {
      const res = await api.post<PlanImportResult>(`/scenarios/${scenario.id}/plan-values/import${dryRun ? '?dry_run=true' : ''}`, form)
      if (dryRun) {
        setPreview(res)
      } else {
        setDone(res)
        onImported?.()
      }
    } catch (err) {
      setError(err)
      setPreview(null)
    } finally {
      setBusy(false)
    }
  }

  const result = done ?? preview
  const changed = result ? result.inserted + result.updated + result.deleted : 0
  return (
    <Dialog
      open
      wide
      title="計画値の CSV の取込"
      onClose={onClose}
      footer={
        done ? (
          <Button variant="primary" onClick={onClose}>
            閉じる
          </Button>
        ) : (
          <>
            <Button onClick={onClose}>キャンセル</Button>
            <Button disabled={!file || busy} onClick={() => send(true)}>
              内容を確認
            </Button>
            <Button variant="primary" disabled={!preview || changed === 0 || busy || reason.trim() === ''} onClick={() => send(false)}>
              {busy ? '処理中…' : '取り込む'}
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        {!done && (
          <>
            <div className="space-y-1 rounded-md bg-slate-50 p-3 text-xs text-slate-600">
              <p>
                取込先: <span className="font-medium text-slate-800">{scenario.name}</span>。「金額を出力」「ドライバー値を出力」した CSV を編集して取り込みます（どちらの CSV かは自動で判定します）。
              </p>
              <p>セルは、空欄 = 変えない、「-」 = 消す、数値 = その値にする。0 は「0 にする」です。CSV に書いていない施策・内訳・月は変えません。</p>
              <p>実績の月と、計算式で反映する内訳の値は取り込みません。内訳・ドライバーは施策の画面で先に作ってください。</p>
              <p>1行でもエラーがあれば取り込みません。</p>
            </div>
            <Field label="CSV ファイル" required>
              {(p) => (
                <Input
                  {...p}
                  type="file"
                  accept=".csv,text/csv"
                  onChange={(e) => {
                    setFile(e.target.files?.[0] ?? null)
                    setPreview(null)
                    setError(null)
                  }}
                  className="file:mr-3 file:rounded file:border-0 file:bg-indigo-50 file:px-2 file:py-1 file:text-indigo-700"
                />
              )}
            </Field>
            <Field label="変更理由" required error={error instanceof ApiError ? error.details.reason : undefined}>
              {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" placeholder="例: 期初計画の入力" />}
            </Field>
          </>
        )}

        {error instanceof ApiError && error.rows.length > 0 ? (
          <div className="space-y-2">
            <ErrorMessage error={error} />
            <div className="max-h-60 overflow-y-auto rounded border border-red-200">
              <Table>
                <thead>
                  <tr>
                    <th className="w-16">行</th>
                    <th>内容</th>
                  </tr>
                </thead>
                <tbody>
                  {error.rows.map((r, i) => (
                    <tr key={i}>
                      <td className="tabular-nums">{r.line}</td>
                      <td className="text-red-700">{r.message}</td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </div>
          </div>
        ) : error && !(error instanceof ApiError && error.details.reason) ? (
          <ErrorMessage error={error} />
        ) : null}

        {result && (
          <div className="space-y-3">
            <p className={done ? 'rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-800' : 'text-sm text-slate-700'}>
              {done ? '取り込みました。' : changed === 0 ? '変更はありません。' : '取り込むと、次のように変わります（まだ保存していません）。'}（{kindLabels[result.kind]}の CSV・
              {result.rows} 行。件数はセルの数）
            </p>
            <div className="grid grid-cols-4 gap-2 text-center text-sm">
              {(
                [
                  ['追加', result.inserted],
                  ['更新', result.updated],
                  ['削除', result.deleted],
                  ['変更なし', result.unchanged],
                ] as const
              ).map(([l, n]) => (
                <div key={l} className="rounded border border-slate-200 py-2">
                  <div className="text-xs text-slate-500">{l}</div>
                  <div className="text-lg font-semibold tabular-nums">{n}</div>
                </div>
              ))}
            </div>
            {result.warnings.length > 0 && (
              <ul className="space-y-1 rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-800" aria-label="注意">
                {result.warnings.map((w) => (
                  <li key={w.kind}>
                    ⚠ {warningLabels[w.kind]}（{w.count} 件）
                  </li>
                ))}
              </ul>
            )}
            {result.activities.length > 0 && (
              <div className="max-h-60 overflow-y-auto rounded border border-slate-200">
                <Table>
                  <thead>
                    <tr>
                      <th className="w-32">コード</th>
                      <th>施策</th>
                      <th className="text-right">変更するセル</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.activities.map((a) => (
                      <tr key={a.activity_id}>
                        <td className="font-mono text-xs">{a.code}</td>
                        <td>{a.name}</td>
                        <td className="text-right tabular-nums">{a.changed}</td>
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}
