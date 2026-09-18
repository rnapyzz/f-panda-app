import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { accountsApi, businessesApi, departmentsApi, type Account, type Business, type Department } from '../../api/dimensions'
import { sheetsApi, submissionsApi, type Binding, type SubmitResult } from '../../api/inputsheet'
import { periodsApi, type Period } from '../../api/periods'
import { scenarioVersionsApi, type ScenarioType, type ScenarioVersion } from '../../api/scenarios'
import {
  buttonPrimary,
  buttonSecondary,
  errorText,
  label as labelClass,
  link,
  mutedText,
  pageHeading,
  select,
  table,
  td,
  th,
} from '../../lib/ui'
import { UniverSheet, type SelectionSnapshot, type UniverSheetHandle } from '../../lib/univer/UniverSheet'
import type { IWorkbookData } from '@univerjs/presets'
import { BindingWizard } from './BindingWizard'

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

export function SheetEditorPage() {
  const { id } = useParams<{ id: string }>()
  const sheetId = Number(id)
  const sheetRef = useRef<UniverSheetHandle>(null)

  const [sheetName, setSheetName] = useState('')
  const [initialSnapshot, setInitialSnapshot] = useState<IWorkbookData | undefined>(undefined)
  const [loaded, setLoaded] = useState(false)

  const [businesses, setBusinesses] = useState<Business[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [periods, setPeriods] = useState<Period[]>([])

  const [bindings, setBindings] = useState<Binding[]>([])
  const [wizardSelection, setWizardSelection] = useState<SelectionSnapshot | null>(null)

  const [scenarioType, setScenarioType] = useState<ScenarioType>('budget')
  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const [versions, setVersions] = useState<ScenarioVersion[]>([])
  const [versionId, setVersionId] = useState<number | ''>('')

  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [submitResult, setSubmitResult] = useState<SubmitResult | null>(null)

  useEffect(() => {
    void Promise.all([sheetsApi.get(sheetId), businessesApi.list(), departmentsApi.list(), accountsApi.list(), periodsApi.list()])
      .then(([sheet, b, d, a, p]) => {
        setSheetName(sheet.name)
        setInitialSnapshot(sheet.snapshot as IWorkbookData)
        setBusinesses(b.filter((x) => x.is_active))
        setDepartments(d.filter((x) => x.is_active))
        setAccounts(a.filter((x) => x.is_active))
        setPeriods(p)
      })
      .catch(() => setError('シートの読み込みに失敗しました'))
      .finally(() => setLoaded(true))
  }, [sheetId])

  async function reloadBindings() {
    try {
      setBindings(await sheetsApi.listBindings(sheetId))
    } catch {
      setError('バインディング一覧の取得に失敗しました')
    }
  }
  useEffect(() => {
    void reloadBindings()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sheetId])

  async function reloadVersions() {
    try {
      const list = await scenarioVersionsApi.list(scenarioType, fiscalYear)
      setVersions(list)
      const current = list.find((v) => v.is_current)
      setVersionId(current?.id ?? '')
    } catch {
      setError('バージョン一覧の取得に失敗しました')
    }
  }
  useEffect(() => {
    void reloadVersions()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scenarioType, fiscalYear])

  async function handleSave() {
    setError(null)
    setMessage(null)
    const snapshot = sheetRef.current?.getSnapshot()
    if (!snapshot) return
    try {
      await sheetsApi.updateSnapshot(sheetId, snapshot)
      setMessage('保存しました')
    } catch {
      setError('保存に失敗しました')
    }
  }

  function handleStartBinding() {
    setError(null)
    const selection = sheetRef.current?.getSelection()
    if (!selection) {
      setError('範囲を選択してください')
      return
    }
    setWizardSelection(selection)
  }

  async function handleSubmit(bindingId: number) {
    setError(null)
    setSubmitResult(null)
    if (versionId === '') {
      setError('提出先のバージョンを選択してください')
      return
    }
    // Submission reads the sheet's last SAVED snapshot, so save first to
    // avoid submitting stale numbers if the user forgot to save.
    await handleSave()
    try {
      const result = await submissionsApi.submit(bindingId, versionId)
      setSubmitResult(result)
    } catch {
      setError('提出に失敗しました')
    }
  }

  if (!loaded) return <p className={`${mutedText} m-8`}>読み込み中...</p>

  return (
    <div className="mx-auto mt-6 max-w-5xl px-4">
      <p className="mb-2">
        <Link to="/sheets" className={link}>
          ← マイシート一覧
        </Link>
      </p>
      <h1 className={pageHeading}>{sheetName}</h1>

      <div className="mb-2 flex gap-2">
        <button type="button" className={buttonPrimary} onClick={() => void handleSave()}>
          保存
        </button>
        <button type="button" className={buttonSecondary} onClick={handleStartBinding}>
          選択範囲からバインディングを作成
        </button>
      </div>
      {message && <p className={mutedText}>{message}</p>}
      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      <UniverSheet ref={sheetRef} initialSnapshot={initialSnapshot} className="mb-4 border border-gray-300 dark:border-gray-600" />

      {wizardSelection && (
        <div className="mb-6">
          <BindingWizard
            selection={wizardSelection}
            businesses={businesses}
            departments={departments}
            accounts={accounts}
            periods={periods}
            onCancel={() => setWizardSelection(null)}
            onSave={async (input) => {
              await sheetsApi.createBinding(sheetId, input)
              setWizardSelection(null)
              await reloadBindings()
            }}
          />
        </div>
      )}

      <h2 className="mb-2 text-lg font-semibold text-gray-900 dark:text-gray-100">バインディング</h2>

      <div className="mb-3 flex flex-wrap items-center gap-4">
        <label className={labelClass}>
          提出先の種別
          <select className={select} value={scenarioType} onChange={(e) => setScenarioType(e.target.value as ScenarioType)}>
            <option value="budget">予算</option>
            <option value="forecast">見込</option>
            <option value="actual">実績</option>
          </select>
        </label>
        <label className={labelClass}>
          会計年度
          <input
            type="number"
            className={`${select} w-24`}
            value={fiscalYear}
            onChange={(e) => setFiscalYear(Number(e.target.value))}
          />
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

      {bindings.length === 0 ? (
        <p className={mutedText}>まだバインディングがありません。範囲を選択して作成してください。</p>
      ) : (
        <table className={table}>
          <thead>
            <tr>
              <th className={th}>名称</th>
              <th className={th}>範囲</th>
              <th className={th}>行の軸</th>
              <th className={th}>列の軸</th>
              <th className={th} />
            </tr>
          </thead>
          <tbody>
            {bindings.map((b) => (
              <tr key={b.id}>
                <td className={td}>{b.name}</td>
                <td className={td}>
                  {b.range_sheet_name}!R{b.start_row}C{b.start_col}:R{b.end_row}C{b.end_col}
                </td>
                <td className={td}>{b.row_axis_dimension}</td>
                <td className={td}>{b.col_axis_dimension}</td>
                <td className={td}>
                  <button type="button" className={buttonSecondary} onClick={() => void handleSubmit(b.id)}>
                    提出
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {submitResult && (
        <div className="mt-4 rounded border border-gray-300 p-3 dark:border-gray-600">
          {submitResult.validation_status === 'ok' ? (
            <p>提出しました（{submitResult.fact_rows_written}件）。</p>
          ) : (
            <div>
              <p className={errorText}>
                シートの構造がバインディング作成時と異なっているため、提出できませんでした。ラベルを確認するか、バインディングを作り直してください。
              </p>
              <ul className="mt-2 list-disc pl-5 text-sm">
                {submitResult.issues.map((issue, i) => (
                  <li key={i}>
                    {issue.axis === 'row' ? '行' : '列'}ラベル: 期待値「{issue.expected}」、実際「{issue.actual}」
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
