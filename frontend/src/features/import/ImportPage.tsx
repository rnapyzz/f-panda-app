import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError } from '../../api/client'
import { importApi, type ColumnMapping, type CommitResult, type ImportBatch, type PreviewResult } from '../../api/import'
import { useScenarioVersions } from '../../hooks/useScenarioVersions'
import { useAuth } from '../auth/useAuth'
import {
  buttonPrimary,
  errorText,
  fieldset,
  input,
  label as labelClass,
  legend,
  link,
  mutedText,
  pageHeading,
  select,
  table,
  td,
  th,
} from '../../lib/ui'

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

const COLUMN_FIELDS: { key: keyof ColumnMapping; label: string }[] = [
  { key: 'business_column', label: '事業の列' },
  { key: 'department_column', label: '部門の列' },
  { key: 'account_column', label: '勘定科目の列' },
  { key: 'period_column', label: '期間の列' },
  { key: 'amount_column', label: '金額の列' },
]

export function ImportPage() {
  const { user } = useAuth()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const { versions, versionId, setVersionId } = useScenarioVersions('actual', fiscalYear)

  const [file, setFile] = useState<File | null>(null)
  const [preview, setPreview] = useState<PreviewResult | null>(null)
  const [mapping, setMapping] = useState<Partial<Record<keyof ColumnMapping, number | ''>>>({})

  const [commitResult, setCommitResult] = useState<CommitResult | null>(null)
  const [batches, setBatches] = useState<ImportBatch[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function reloadBatches(forVersionId: number | '') {
    if (forVersionId === '') {
      setBatches([])
      return
    }
    try {
      setBatches(await importApi.listBatches(forVersionId))
    } catch {
      setError('取り込み履歴の取得に失敗しました')
    }
  }
  useEffect(() => {
    void reloadBatches(versionId)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [versionId])

  async function handleFileChange() {
    setError(null)
    setCommitResult(null)
    setPreview(null)
    setMapping({})
    const selected = fileInputRef.current?.files?.[0] ?? null
    setFile(selected)
    if (!selected) return
    try {
      setBusy(true)
      setPreview(await importApi.preview(selected))
    } catch {
      setError('ファイルの解析に失敗しました。CSVまたはXLSXファイルを選択してください。')
    } finally {
      setBusy(false)
    }
  }

  async function handleCommit() {
    setError(null)
    setCommitResult(null)
    if (!file || versionId === '') {
      setError('対象バージョンとファイルを選択してください')
      return
    }
    const fullMapping = {} as ColumnMapping
    for (const { key, label } of COLUMN_FIELDS) {
      const value = mapping[key]
      if (value === undefined || value === '') {
        setError(`${label}を選択してください`)
        return
      }
      fullMapping[key] = value
    }
    try {
      setBusy(true)
      const result = await importApi.commit(file, versionId, fullMapping)
      setCommitResult(result)
      await reloadBatches(versionId)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '取り込みに失敗しました')
    } finally {
      setBusy(false)
    }
  }

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>実績インポートは事務局管理者のみ利用できます。</p>
  }

  return (
    <div>
      <h1 className={pageHeading}>実績インポート（CSV/XLSX）</h1>

      <fieldset className={fieldset}>
        <legend className={legend}>対象バージョン（実績）</legend>
        <div className="flex flex-wrap items-center gap-4">
          <label className={labelClass}>
            会計年度
            <input type="number" className={`${input} w-24`} value={fiscalYear} onChange={(e) => setFiscalYear(Number(e.target.value))} />
          </label>
          <label className={labelClass}>
            バージョン
            <select className={select} value={versionId} onChange={(e) => setVersionId(e.target.value ? Number(e.target.value) : '')}>
              <option value="">選択してください</option>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.version_label} {v.is_current ? '（現行）' : ''}
                </option>
              ))}
            </select>
          </label>
        </div>
        {versions.length === 0 && (
          <p className={mutedText}>
            バージョンがありません。
            <Link to="/versions" className={link}>
              バージョン管理
            </Link>
            で作成してください。
          </p>
        )}
      </fieldset>

      <fieldset className={fieldset}>
        <legend className={legend}>ファイル選択</legend>
        <input ref={fileInputRef} type="file" accept=".csv,.xlsx" onChange={() => void handleFileChange()} />
        {busy && <p className={mutedText}>処理中...</p>}
      </fieldset>

      {preview && (
        <fieldset className={fieldset}>
          <legend className={legend}>列マッピング</legend>
          <p className={mutedText}>{preview.total_rows} 行のデータを検出しました。各項目がどの列にあるかを選択してください。</p>

          <div className="flex flex-wrap gap-4">
            {COLUMN_FIELDS.map(({ key, label }) => (
              <label key={key} className={labelClass}>
                {label}
                <select
                  className={select}
                  value={mapping[key] ?? ''}
                  onChange={(e) => setMapping((m) => ({ ...m, [key]: e.target.value ? Number(e.target.value) : '' }))}
                >
                  <option value="">選択してください</option>
                  {preview.headers.map((h, i) => (
                    <option key={i} value={i}>
                      {h || `(列${i + 1})`}
                    </option>
                  ))}
                </select>
              </label>
            ))}
          </div>

          <div className="overflow-x-auto">
            <table className={table}>
              <thead>
                <tr>
                  {preview.headers.map((h, i) => (
                    <th key={i} className={th}>
                      {h || `列${i + 1}`}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {preview.sample_rows.map((row, i) => (
                  <tr key={i}>
                    {row.map((cell, j) => (
                      <td key={j} className={td}>
                        {cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <button type="button" className={buttonPrimary} onClick={() => void handleCommit()} disabled={busy}>
            取り込む
          </button>
        </fieldset>
      )}

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      {commitResult && (
        <div className="mb-4 rounded border border-gray-300 p-3 dark:border-gray-600">
          <p>
            取り込み結果：{commitResult.row_count} 件成功 / {commitResult.error_count} 件エラー（ステータス: {commitResult.status}）
          </p>
          {commitResult.errors.length > 0 && (
            <ul className="mt-2 list-disc pl-5 text-sm">
              {commitResult.errors.map((e, i) => (
                <li key={i}>
                  {e.row_number}行目: {e.message}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <h2 className="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">取り込み履歴</h2>
      {batches.length === 0 ? (
        <p className={mutedText}>このバージョンへの取り込み履歴はありません。</p>
      ) : (
        <table className={table}>
          <thead>
            <tr>
              <th className={th}>ファイル名</th>
              <th className={th}>ステータス</th>
              <th className={th}>成功</th>
              <th className={th}>エラー</th>
              <th className={th}>日時</th>
            </tr>
          </thead>
          <tbody>
            {batches.map((b) => (
              <tr key={b.id}>
                <td className={td}>{b.original_filename}</td>
                <td className={td}>{b.status}</td>
                <td className={td}>{b.row_count}</td>
                <td className={td}>{b.error_count}</td>
                <td className={td}>{new Date(b.created_at).toLocaleString('ja-JP')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
