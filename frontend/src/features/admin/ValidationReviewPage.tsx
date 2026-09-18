import { Fragment, useEffect, useState } from 'react'
import { submissionReviewApi, type ReviewSubmission } from '../../api/inputsheet'
import { useAuth } from '../auth/useAuth'
import { buttonSecondary, errorText, label as labelClass, mutedText, pageHeading, select, table, td, th } from '../../lib/ui'

const SCENARIO_TYPE_LABELS: Record<string, string> = { budget: '予算', forecast: '見込', actual: '実績' }
const VALIDATION_LABELS: Record<string, string> = { ok: 'OK', warning: '警告', error: 'エラー' }

function validationBadgeClass(status: string) {
  if (status === 'error') return 'rounded px-2 py-0.5 text-xs font-medium bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
  if (status === 'warning') return 'rounded px-2 py-0.5 text-xs font-medium bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300'
  return 'rounded px-2 py-0.5 text-xs font-medium bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300'
}

export function ValidationReviewPage() {
  const { user } = useAuth()
  const [submissions, setSubmissions] = useState<ReviewSubmission[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState<'all' | 'ok' | 'warning' | 'error'>('all')
  const [expandedId, setExpandedId] = useState<number | null>(null)

  useEffect(() => {
    submissionReviewApi
      .list()
      .then(setSubmissions)
      .catch(() => setError('提出一覧の取得に失敗しました'))
      .finally(() => setLoading(false))
  }, [])

  if (user?.role !== 'office_admin') {
    return <p className={mutedText}>バインディング検証結果は事務局管理者のみ利用できます。</p>
  }
  if (loading) return <p className={mutedText}>読み込み中...</p>

  const filtered = filter === 'all' ? submissions : submissions.filter((s) => s.validation_status === filter)

  return (
    <div>
      <h1 className={pageHeading}>バインディング検証結果</h1>
      <p className={mutedText}>全社の提出（直近500件）を横断して、シート形状チェックの結果を確認できます。</p>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      <div className="my-4">
        <label className={labelClass}>
          検証結果で絞り込み
          <select className={select} value={filter} onChange={(e) => setFilter(e.target.value as typeof filter)}>
            <option value="all">すべて</option>
            <option value="ok">OK</option>
            <option value="warning">警告</option>
            <option value="error">エラー</option>
          </select>
        </label>
      </div>

      {filtered.length === 0 ? (
        <p className={mutedText}>該当する提出はありません。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th className={th}>提出者</th>
                <th className={th}>シート</th>
                <th className={th}>バインディング</th>
                <th className={th}>シナリオ</th>
                <th className={th}>検証結果</th>
                <th className={th}>提出日時</th>
                <th className={th} />
              </tr>
            </thead>
            <tbody>
              {filtered.map((s) => (
                <Fragment key={s.id}>
                  <tr>
                    <td className={td}>{s.submitted_by_name}</td>
                    <td className={td}>{s.sheet_name}</td>
                    <td className={td}>{s.binding_name}</td>
                    <td className={td}>
                      {SCENARIO_TYPE_LABELS[s.scenario_type] ?? s.scenario_type} {s.fiscal_year}年度 / {s.version_label}
                    </td>
                    <td className={td}>
                      <span className={validationBadgeClass(s.validation_status)}>{VALIDATION_LABELS[s.validation_status]}</span>
                    </td>
                    <td className={td}>{new Date(s.submitted_at).toLocaleString('ja-JP')}</td>
                    <td className={td}>
                      {s.validation_detail && s.validation_detail.length > 0 && (
                        <button type="button" className={buttonSecondary} onClick={() => setExpandedId(expandedId === s.id ? null : s.id)}>
                          {expandedId === s.id ? '閉じる' : '詳細'}
                        </button>
                      )}
                    </td>
                  </tr>
                  {expandedId === s.id && s.validation_detail && (
                    <tr>
                      <td className={td} colSpan={7}>
                        <ul className="list-disc pl-5 text-sm">
                          {s.validation_detail.map((issue, i) => (
                            <li key={i}>
                              {issue.axis === 'row' ? '行' : '列'} {issue.axis_index + 1}: 期待値「{issue.expected}」が「{issue.actual}」に変更されています
                            </li>
                          ))}
                        </ul>
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
