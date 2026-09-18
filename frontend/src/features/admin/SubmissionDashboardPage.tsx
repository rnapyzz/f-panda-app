import { useEffect, useState } from 'react'
import { scenarioVersionsApi, type ScenarioType, type ScenarioVersion } from '../../api/scenarios'
import { submissionStatusApi, type ScopeStatus } from '../../api/submissionStatus'
import { useAuth } from '../auth/useAuth'
import { errorText, input, label as labelClass, mutedText, pageHeading, select, table, td, th } from '../../lib/ui'

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

const SCENARIO_TYPE_LABELS: Record<ScenarioType, string> = { budget: '予算', forecast: '見込', actual: '実績' }

const VALIDATION_LABELS: Record<string, string> = { ok: 'OK', warning: '警告', error: 'エラー' }

function validationBadgeClass(status?: string) {
  if (status === 'error') return 'rounded px-2 py-0.5 text-xs font-medium bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
  if (status === 'warning') return 'rounded px-2 py-0.5 text-xs font-medium bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
  return 'rounded px-2 py-0.5 text-xs font-medium bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
}

export function SubmissionDashboardPage() {
  const { user } = useAuth()
  const [scenarioType, setScenarioType] = useState<ScenarioType>('budget')
  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const [versions, setVersions] = useState<ScenarioVersion[]>([])
  const [versionId, setVersionId] = useState<number | ''>('')
  const [statuses, setStatuses] = useState<ScopeStatus[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

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

  useEffect(() => {
    if (versionId === '') {
      setStatuses(null)
      return
    }
    setLoading(true)
    submissionStatusApi
      .get(versionId)
      .then((rows) => {
        setStatuses(rows)
        setError(null)
      })
      .catch(() => setError('提出状況の取得に失敗しました'))
      .finally(() => setLoading(false))
  }, [versionId])

  const submittedCount = statuses?.filter((s) => s.submitted).length ?? 0

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>提出状況ダッシュボードは事務局管理者のみ利用できます。</p>
  }

  return (
    <div>
      <h1 className={pageHeading}>提出状況ダッシュボード</h1>
      <p className={mutedText}>担当割当てを基準に、各事業×部門の予算・見込・実績の提出状況を確認できます。</p>

      <div className="my-4 flex flex-wrap items-center gap-4">
        <label className={labelClass}>
          シナリオ種別
          <select className={select} value={scenarioType} onChange={(e) => setScenarioType(e.target.value as ScenarioType)}>
            <option value="budget">予算</option>
            <option value="forecast">見込</option>
            <option value="actual">実績</option>
          </select>
        </label>
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

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      {loading && <p className={mutedText}>読み込み中...</p>}

      {!loading && statuses && (
        <>
          <p className={`${mutedText} mb-2`}>
            {statuses.length} 件中 {submittedCount} 件提出済み（{SCENARIO_TYPE_LABELS[scenarioType]} {fiscalYear}年度）
          </p>
          {statuses.length === 0 ? (
            <p className={mutedText}>担当割当てが登録されている事業×部門がありません。</p>
          ) : (
            <div className="overflow-x-auto">
              <table className={table}>
                <thead>
                  <tr>
                    <th className={th}>事業</th>
                    <th className={th}>部門</th>
                    <th className={th}>担当者</th>
                    <th className={th}>提出状況</th>
                    <th className={th}>検証結果</th>
                    <th className={th}>提出日時</th>
                  </tr>
                </thead>
                <tbody>
                  {statuses.map((s) => (
                    <tr key={`${s.business_id}-${s.department_id}`}>
                      <td className={td}>{s.business_name}</td>
                      <td className={td}>{s.department_name}</td>
                      <td className={td}>{s.assigned_users.map((u) => u.name).join(', ')}</td>
                      <td className={td}>
                        {s.submitted ? (
                          <span className="rounded bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/40 dark:text-green-300">
                            提出済み
                          </span>
                        ) : (
                          <span className="rounded bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                            未提出
                          </span>
                        )}
                      </td>
                      <td className={td}>
                        {s.validation_status ? (
                          <span className={validationBadgeClass(s.validation_status)}>{VALIDATION_LABELS[s.validation_status]}</span>
                        ) : (
                          '—'
                        )}
                      </td>
                      <td className={td}>{s.submitted_at ? new Date(s.submitted_at).toLocaleString('ja-JP') : '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}
