import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { ImportResult, Scenario } from '../../api/types'
import { Button, Dialog, ErrorMessage, Field, Input, Table, Textarea } from '../../components/ui'
import { formatYen, yearMonthLabel } from '../../lib/format'

/**
 * 実績 CSV の取込ダイアログ。
 * 「内容を確認」で dry run（保存しない検証と集計）を行い、結果を確認してから「取り込む」。
 */
export function ActualsImportDialog({ scenario, onClose }: { scenario: Scenario; onClose: () => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [reason, setReason] = useState('')
  const [preview, setPreview] = useState<ImportResult | null>(null)
  const [done, setDone] = useState<ImportResult | null>(null)
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
      const res = await api.post<ImportResult>(`/scenarios/${scenario.id}/actuals/import${dryRun ? '?dry_run=true' : ''}`, form)
      if (dryRun) setPreview(res)
      else setDone(res)
    } catch (err) {
      setError(err)
      setPreview(null)
    } finally {
      setBusy(false)
    }
  }

  const result = done ?? preview
  return (
    <Dialog
      open
      wide
      title="実績 CSV の取込"
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
            <Button variant="primary" disabled={!preview || busy || reason.trim() === ''} onClick={() => send(false)}>
              {busy ? '処理中…' : '取り込む'}
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        {!done && (
          <>
            <div className="rounded-md bg-slate-50 p-3 text-xs text-slate-600">
              <p>
                形式: UTF-8 の CSV。1行目はヘッダー <code className="font-mono">target_month,activity_code,subject_code,amount</code>
              </p>
              <p className="mt-1">
                <code className="font-mono">activity_code</code> には、施策コードまたは施策に登録した外部コード（案件番号など）を書けます。同じ施策の行は合算します。
              </p>
              <p className="mt-1">CSV に含まれる月の実績は、CSV の内容で置き換えます。1行でもエラーがあれば取り込みません。</p>
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
            <Field label="変更理由" required hint="取り込む前に入力してください（例: 2026-09 実績取込）" error={error instanceof ApiError ? error.details.reason : undefined}>
              {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" />}
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
              {done ? '取り込みました。' : '取り込むと、次のように変わります（まだ保存していません）。'}
              {result.months.map(yearMonthLabel).join('・')} の {result.rows} 行（合算後 {result.facts} 件）
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
            <Table>
              <thead>
                <tr>
                  <th>月</th>
                  <th className="text-right">収益の合計</th>
                  <th className="text-right">費用の合計</th>
                </tr>
              </thead>
              <tbody>
                {result.totals.map((t) => (
                  <tr key={t.month}>
                    <td>{yearMonthLabel(t.month)}</td>
                    <td className="text-right tabular-nums">{formatYen(t.revenue)}</td>
                    <td className="text-right tabular-nums">{formatYen(t.expense)}</td>
                  </tr>
                ))}
              </tbody>
            </Table>
            <p className="text-xs text-slate-500">合計は、会計システムの数字と突き合わせる際に使えます。</p>
          </div>
        )}
      </div>
    </Dialog>
  )
}
