import { useState, type ReactNode } from 'react'
import { api, ApiError, download } from '../api/client'
import { Button, Dialog, ErrorMessage, Field, Input, Table, Textarea } from './ui'

type ImportResult = { dry_run: boolean; rows: number; inserted: number; updated: number; unchanged: number }

/**
 * マスタ・施策の CSV エクスポートとインポートのボタン。
 * resource は API のパス（例: "organizations"）。インポートは canImport のときだけ表示する。
 */
export function CsvActions({
  resource,
  label,
  canImport,
  columns,
  notes,
  onImported,
}: {
  resource: string
  label: string
  canImport: boolean
  columns: string
  notes?: ReactNode
  onImported: () => void
}) {
  const [open, setOpen] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState<unknown>(null)

  const exportCsv = async () => {
    setExporting(true)
    try {
      await download(`/${resource}/export`)
    } catch (err) {
      setExportError(err)
    } finally {
      setExporting(false)
    }
  }

  return (
    <>
      <Button onClick={exportCsv} disabled={exporting}>
        {exporting ? 'エクスポート中…' : 'CSV エクスポート'}
      </Button>
      {exportError ? (
        <Dialog
          open
          title="エクスポートできませんでした"
          onClose={() => setExportError(null)}
          footer={
            <Button variant="primary" onClick={() => setExportError(null)}>
              閉じる
            </Button>
          }
        >
          <ErrorMessage error={exportError} />
          {exportError instanceof ApiError && exportError.status === 404 && (
            <p className="mt-2 text-xs text-slate-500">サーバーが古い可能性があります。開発環境では `make up` で API を作り直してください。</p>
          )}
        </Dialog>
      ) : null}
      {canImport && <Button onClick={() => setOpen(true)}>CSV インポート</Button>}
      {open && (
        <CsvImportDialog
          resource={resource}
          label={label}
          columns={columns}
          notes={notes}
          onClose={() => setOpen(false)}
          onImported={onImported}
        />
      )}
    </>
  )
}

/** CSV の取込ダイアログ。「内容を確認」（dry run）で件数を確かめてから取り込む */
function CsvImportDialog({
  resource,
  label,
  columns,
  notes,
  onClose,
  onImported,
}: {
  resource: string
  label: string
  columns: string
  notes?: ReactNode
  onClose: () => void
  onImported: () => void
}) {
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
      const res = await api.post<ImportResult>(`/${resource}/import${dryRun ? '?dry_run=true' : ''}`, form)
      if (dryRun) {
        setPreview(res)
      } else {
        setDone(res)
        onImported()
      }
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
      title={`${label}の CSV インポート`}
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
            <div className="space-y-1 rounded-md bg-slate-50 p-3 text-xs text-slate-600">
              <p>
                UTF-8 の CSV。1行目はヘッダー <code className="font-mono break-all">{columns}</code>（列の順番は自由）
              </p>
              <p>CSV の行を追加・更新します。CSV にないデータは削除しません。1行でもエラーがあれば取り込みません。</p>
              <p>エクスポートした CSV を編集して取り込むのが簡単です。</p>
              {notes}
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
              {(p) => <Textarea {...p} value={reason} onChange={(e) => setReason(e.target.value)} className="min-h-12" placeholder="例: 2026年度の組織改編" />}
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
              {done ? '取り込みました。' : '取り込むと、次のように変わります（まだ保存していません）。'}（{result.rows} 行）
            </p>
            <div className="grid grid-cols-3 gap-2 text-center text-sm">
              {(
                [
                  ['追加', result.inserted],
                  ['更新', result.updated],
                  ['変更なし', result.unchanged],
                ] as const
              ).map(([l, n]) => (
                <div key={l} className="rounded border border-slate-200 py-2">
                  <div className="text-xs text-slate-500">{l}</div>
                  <div className="text-lg font-semibold tabular-nums">{n}</div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
