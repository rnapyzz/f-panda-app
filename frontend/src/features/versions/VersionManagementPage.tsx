import { useEffect, useState, type FormEvent } from 'react'
import { ApiError } from '../../api/client'
import { periodsApi, type Period } from '../../api/periods'
import { scenarioVersionsApi, type ScenarioType, type ScenarioVersion } from '../../api/scenarios'
import {
  buttonPrimary,
  buttonSecondary,
  errorText,
  fieldset,
  input,
  label as labelClass,
  legend,
  mutedText,
  pageHeading,
  select,
  table,
  td,
  th,
} from '../../lib/ui'
import { useAuth } from '../auth/useAuth'

const SCENARIO_TYPES: ScenarioType[] = ['budget', 'forecast', 'actual']

const SCENARIO_LABELS: Record<ScenarioType, string> = {
  budget: '予算',
  forecast: '見込',
  actual: '実績',
}

const STATUS_LABELS: Record<ScenarioVersion['status'], string> = {
  draft: '下書き',
  submitted: '提出済み',
  locked: '確定済み',
}

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

function formatDateTime(s?: string): string {
  return s ? new Date(s).toLocaleString('ja-JP') : '—'
}

export function VersionManagementPage() {
  const { user } = useAuth()

  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const [periods, setPeriods] = useState<Period[]>([])
  const [versions, setVersions] = useState<ScenarioVersion[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [newScenarioType, setNewScenarioType] = useState<ScenarioType>('budget')
  const [newVersionLabel, setNewVersionLabel] = useState('')
  const [newAsOfPeriodId, setNewAsOfPeriodId] = useState<number | ''>('')

  useEffect(() => {
    void periodsApi.list().then(setPeriods)
  }, [])

  async function reload() {
    setLoading(true)
    try {
      const lists = await Promise.all(SCENARIO_TYPES.map((t) => scenarioVersionsApi.list(t, fiscalYear)))
      setVersions(lists.flat())
      setError(null)
    } catch {
      setError('バージョン一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fiscalYear])

  const periodsInYear = periods.filter((p) => p.fiscal_year === fiscalYear)

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      await scenarioVersionsApi.create({
        scenario_type: newScenarioType,
        fiscal_year: fiscalYear,
        version_label: newVersionLabel,
        as_of_period_id: newScenarioType === 'forecast' && newAsOfPeriodId !== '' ? newAsOfPeriodId : undefined,
      })
      setNewVersionLabel('')
      setNewAsOfPeriodId('')
      await reload()
    } catch {
      setError('バージョンの作成に失敗しました')
    }
  }

  async function handleSubmit(v: ScenarioVersion) {
    setError(null)
    try {
      await scenarioVersionsApi.submit(v.id)
      await reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '提出に失敗しました')
    }
  }

  async function handleLock(v: ScenarioVersion) {
    setError(null)
    try {
      await scenarioVersionsApi.lock(v.id)
      await reload()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '確定に失敗しました')
    }
  }

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>バージョン管理は事務局管理者のみ利用できます。</p>
  }

  return (
    <div>
      <h1 className={pageHeading}>バージョン管理</h1>

      <fieldset className={fieldset}>
        <legend className={legend}>新規バージョン作成</legend>
        <form onSubmit={handleCreate} className="flex flex-wrap items-center gap-2">
          <select className={select} value={newScenarioType} onChange={(e) => setNewScenarioType(e.target.value as ScenarioType)}>
            <option value="budget">予算</option>
            <option value="forecast">見込</option>
            <option value="actual">実績</option>
          </select>
          <label className={labelClass}>
            会計年度
            <input
              type="number"
              className={`${input} w-24`}
              value={fiscalYear}
              onChange={(e) => setFiscalYear(Number(e.target.value))}
            />
          </label>
          <input
            className={input}
            placeholder="バージョン名"
            value={newVersionLabel}
            onChange={(e) => setNewVersionLabel(e.target.value)}
            required
          />
          {newScenarioType === 'forecast' && (
            <select
              className={select}
              value={newAsOfPeriodId}
              onChange={(e) => setNewAsOfPeriodId(e.target.value ? Number(e.target.value) : '')}
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
          <button type="submit" className={buttonPrimary}>
            バージョンを作成
          </button>
        </form>
      </fieldset>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      {loading ? (
        <p className={mutedText}>読み込み中...</p>
      ) : versions.length === 0 ? (
        <p className={mutedText}>{fiscalYear}年度のバージョンはまだありません。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th className={th}>種別</th>
                <th className={th}>バージョン名</th>
                <th className={th}>状態</th>
                <th className={th}>現行</th>
                <th className={th}>提出日時</th>
                <th className={th}>確定日時</th>
                <th className={th} />
              </tr>
            </thead>
            <tbody>
              {versions.map((v) => (
                <tr key={v.id}>
                  <td className={td}>{SCENARIO_LABELS[v.scenario_type]}</td>
                  <td className={td}>{v.version_label}</td>
                  <td className={td}>{STATUS_LABELS[v.status]}</td>
                  <td className={td}>{v.is_current ? '現行' : ''}</td>
                  <td className={td}>{formatDateTime(v.submitted_at)}</td>
                  <td className={td}>{formatDateTime(v.locked_at)}</td>
                  <td className={td}>
                    {v.status === 'draft' && (
                      <button type="button" className={buttonSecondary} onClick={() => void handleSubmit(v)}>
                        提出
                      </button>
                    )}
                    {v.status === 'submitted' && (
                      <button type="button" className={buttonSecondary} onClick={() => void handleLock(v)}>
                        確定
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
