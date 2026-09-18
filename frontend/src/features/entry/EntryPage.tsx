import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { accountsApi, businessesApi, departmentsApi, type Account, type Business, type Department } from '../../api/dimensions'
import { factsApi } from '../../api/facts'
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
    const list = await scenarioVersionsApi.list(scenarioType, fiscalYear)
    setVersions(list)
    const current = list.find((v) => v.is_current)
    setVersionId(current?.id ?? '')
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
    <div style={{ maxWidth: 560, margin: '40px auto' }}>
      <p>
        <Link to="/">← ダッシュボード</Link>
      </p>
      <h1>データ入力（簡易フォーム）</h1>

      <fieldset>
        <legend>対象バージョン</legend>
        <label>
          種別{' '}
          <select value={scenarioType} onChange={(e) => setScenarioType(e.target.value as ScenarioType)}>
            <option value="budget">予算</option>
            <option value="forecast">見込</option>
            <option value="actual">実績</option>
          </select>
        </label>{' '}
        <label>
          会計年度{' '}
          <input
            type="number"
            value={fiscalYear}
            onChange={(e) => setFiscalYear(Number(e.target.value))}
            style={{ width: 80 }}
          />
        </label>

        <div style={{ marginTop: 8 }}>
          <label>
            バージョン{' '}
            <select value={versionId} onChange={(e) => setVersionId(e.target.value ? Number(e.target.value) : '')}>
              <option value="">選択してください</option>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.version_label} {v.is_current ? '（現行）' : ''}
                </option>
              ))}
            </select>
          </label>
        </div>

        {canCreateVersion && (
          <form onSubmit={handleCreateVersion} style={{ marginTop: 8 }}>
            <input
              placeholder={`新しい${SCENARIO_LABELS[scenarioType]}バージョン名`}
              value={newVersionLabel}
              onChange={(e) => setNewVersionLabel(e.target.value)}
              required
            />
            {scenarioType === 'forecast' && (
              <select
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
            <button type="submit">バージョンを作成</button>
          </form>
        )}
      </fieldset>

      <form onSubmit={handleSubmitEntry} style={{ marginTop: 16 }}>
        <fieldset>
          <legend>入力</legend>
          <div>
            <label>
              事業{' '}
              <select value={businessId} onChange={(e) => setBusinessId(e.target.value ? Number(e.target.value) : '')}>
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
            <label>
              部門{' '}
              <select
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
            <label>
              勘定科目{' '}
              <select value={accountId} onChange={(e) => setAccountId(e.target.value ? Number(e.target.value) : '')}>
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
            <label>
              期間{' '}
              <select value={periodId} onChange={(e) => setPeriodId(e.target.value ? Number(e.target.value) : '')}>
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
            <label>
              金額{' '}
              <input type="number" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} required />
            </label>
          </div>
          <button type="submit">保存</button>
        </fieldset>
      </form>

      {message && <p>{message}</p>}
      {error && <p role="alert">{error}</p>}
    </div>
  )
}
