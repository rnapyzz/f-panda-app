import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { accountsApi, businessesApi, departmentsApi, type Account, type Business, type Department } from '../../api/dimensions'
import { factsApi } from '../../api/facts'
import {
  buttonPrimary,
  buttonSecondary,
  errorText,
  fieldset,
  input,
  label,
  legend,
  mutedText,
  pageHeading,
  select,
} from '../../lib/ui'
import { periodsApi, type Period } from '../../api/periods'
import { scenarioVersionsApi, type ScenarioType, type ScenarioVersion } from '../../api/scenarios'
import { useAuth } from '../auth/useAuth'

const SCENARIO_LABELS: Record<ScenarioType, string> = {
  budget: '予算',
  forecast: '見込',
  actual: '実績',
}

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

export function EntryPage() {
  const { user } = useAuth()
  const canCreateVersion = user?.role === 'office_admin'

  const [businesses, setBusinesses] = useState<Business[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [periods, setPeriods] = useState<Period[]>([])

  const [scenarioType, setScenarioType] = useState<ScenarioType>('budget')
  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const [versions, setVersions] = useState<ScenarioVersion[]>([])
  const [versionId, setVersionId] = useState<number | ''>('')
  const [newVersionLabel, setNewVersionLabel] = useState('')
  const [newVersionAsOfPeriodId, setNewVersionAsOfPeriodId] = useState<number | ''>('')

  const [businessId, setBusinessId] = useState<number | ''>('')
  const [departmentId, setDepartmentId] = useState<number | ''>('')
  const [accountId, setAccountId] = useState<number | ''>('')
  const [periodId, setPeriodId] = useState<number | ''>('')
  const [amount, setAmount] = useState('')

  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void Promise.all([businessesApi.list(), departmentsApi.list(), accountsApi.list(), periodsApi.list()]).then(
      ([b, d, a, p]) => {
        setBusinesses(b.filter((x) => x.is_active))
        setDepartments(d.filter((x) => x.is_active))
        setAccounts(a.filter((x) => x.is_active))
        setPeriods(p)
      },
    )
  }, [])

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

  const periodsInYear = useMemo(() => periods.filter((p) => p.fiscal_year === fiscalYear), [periods, fiscalYear])

  async function handleCreateVersion(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const { id } = await scenarioVersionsApi.create({
        scenario_type: scenarioType,
        fiscal_year: fiscalYear,
        version_label: newVersionLabel,
        as_of_period_id: scenarioType === 'forecast' && newVersionAsOfPeriodId !== '' ? newVersionAsOfPeriodId : undefined,
      })
      setNewVersionLabel('')
      setNewVersionAsOfPeriodId('')
      await reloadVersions()
      setVersionId(id)
      setMessage('新しいバージョンを作成しました')
    } catch {
      setError('バージョンの作成に失敗しました')
    }
  }

  async function handleSubmitEntry(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setMessage(null)
    if (versionId === '' || businessId === '' || departmentId === '' || accountId === '' || periodId === '') {
      setError('すべての項目を選択してください')
      return
    }
    try {
      await factsApi.upsertEntry({
        scenario_version_id: versionId,
        business_id: businessId,
        department_id: departmentId,
        account_id: accountId,
        period_id: periodId,
        amount: Number(amount),
      })
      setMessage('保存しました')
      setAmount('')
    } catch {
      setError('保存に失敗しました')
    }
  }

  return (
    <div className="max-w-xl">
      <h1 className={pageHeading}>データ入力（簡易フォーム）</h1>

      <fieldset className={fieldset}>
        <legend className={legend}>対象バージョン</legend>
        <div className="flex flex-wrap items-center gap-4">
          <label className={label}>
            種別
            <select className={select} value={scenarioType} onChange={(e) => setScenarioType(e.target.value as ScenarioType)}>
              <option value="budget">予算</option>
              <option value="forecast">見込</option>
              <option value="actual">実績</option>
            </select>
          </label>
          <label className={label}>
            会計年度
            <input
              type="number"
              className={`${input} w-24`}
              value={fiscalYear}
              onChange={(e) => setFiscalYear(Number(e.target.value))}
            />
          </label>
        </div>

        <label className={label}>
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

        {canCreateVersion && (
          <form onSubmit={handleCreateVersion} className="flex flex-wrap items-center gap-2 border-t border-gray-200 pt-3 dark:border-gray-700">
            <input
              className={input}
              placeholder={`新しい${SCENARIO_LABELS[scenarioType]}バージョン名`}
              value={newVersionLabel}
              onChange={(e) => setNewVersionLabel(e.target.value)}
              required
            />
            {scenarioType === 'forecast' && (
              <select
                className={select}
                value={newVersionAsOfPeriodId}
                onChange={(e) => setNewVersionAsOfPeriodId(e.target.value ? Number(e.target.value) : '')}
                required
              >
                <option value="">見込作成時点の月</option>
                {periodsInYear.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            )}
            <button type="submit" className={buttonSecondary}>
              バージョンを作成
            </button>
          </form>
        )}
      </fieldset>

      <form onSubmit={handleSubmitEntry} className="mt-4">
        <fieldset className={fieldset}>
          <legend className={legend}>入力</legend>
          <div>
            <label className={label}>
              事業
              <select className={select} value={businessId} onChange={(e) => setBusinessId(e.target.value ? Number(e.target.value) : '')}>
                <option value="">選択してください</option>
                {businesses.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <div>
            <label className={label}>
              部門
              <select
                className={select}
                value={departmentId}
                onChange={(e) => setDepartmentId(e.target.value ? Number(e.target.value) : '')}
              >
                <option value="">選択してください</option>
                {departments.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <div>
            <label className={label}>
              勘定科目
              <select className={select} value={accountId} onChange={(e) => setAccountId(e.target.value ? Number(e.target.value) : '')}>
                <option value="">選択してください</option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <div>
            <label className={label}>
              期間
              <select className={select} value={periodId} onChange={(e) => setPeriodId(e.target.value ? Number(e.target.value) : '')}>
                <option value="">選択してください</option>
                {periodsInYear.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <div>
            <label className={label}>
              金額
              <input
                type="number"
                step="0.01"
                className={`${input} w-40`}
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                required
              />
            </label>
          </div>
          <button type="submit" className={buttonPrimary}>
            保存
          </button>
        </fieldset>
      </form>

      {message && <p className={mutedText}>{message}</p>}
      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}
    </div>
  )
}
