import { Fragment, useEffect, useMemo, useState } from 'react'
import { accountsApi, businessesApi, departmentsApi, type Account, type Business, type Department } from '../../api/dimensions'
import { factsApi, type VarianceRow } from '../../api/facts'
import { errorText, input, label, mutedText, pageHeading, select, table, td, tdRight, th } from '../../lib/ui'
import { groupVarianceRowsByDimension } from './groupVarianceRows'

const currentFiscalYear = new Date().getMonth() + 1 >= 4 ? new Date().getFullYear() : new Date().getFullYear() - 1

function formatAmount(v: number | null): string {
  if (v === null) return '—'
  return v.toLocaleString('ja-JP', { minimumFractionDigits: 0, maximumFractionDigits: 2 })
}

export function VariancePage() {
  const [businesses, setBusinesses] = useState<Business[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])

  const [fiscalYear, setFiscalYear] = useState(currentFiscalYear)
  const [businessId, setBusinessId] = useState<number | ''>('')
  const [departmentId, setDepartmentId] = useState<number | ''>('')
  const [accountId, setAccountId] = useState<number | ''>('')

  const [rows, setRows] = useState<VarianceRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [showPeriodBreakdown, setShowPeriodBreakdown] = useState(false)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())

  useEffect(() => {
    void Promise.all([businessesApi.list(), departmentsApi.list(), accountsApi.list()]).then(([b, d, a]) => {
      setBusinesses(b)
      setDepartments(d)
      setAccounts(a)
    })
  }, [])

  useEffect(() => {
    setLoading(true)
    factsApi
      .varianceReport(fiscalYear, {
        businessId: businessId || undefined,
        departmentId: departmentId || undefined,
        accountId: accountId || undefined,
      })
      .then((r) => {
        setRows(r)
        setError(null)
        setExpandedGroups(new Set())
      })
      .catch(() => setError('予実差異レポートの取得に失敗しました'))
      .finally(() => setLoading(false))
  }, [fiscalYear, businessId, departmentId, accountId])

  const groups = useMemo(() => groupVarianceRowsByDimension(rows), [rows])

  function toggleGroup(key: string) {
    setExpandedGroups((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  return (
    <div>
      <h1 className={pageHeading}>予実差異レポート</h1>

      <div className="mb-4 flex flex-wrap items-center gap-4">
        <label className={label}>
          会計年度
          <input
            type="number"
            className={`${input} w-24`}
            value={fiscalYear}
            onChange={(e) => setFiscalYear(Number(e.target.value))}
          />
        </label>
        <label className={label}>
          事業
          <select className={select} value={businessId} onChange={(e) => setBusinessId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">すべて</option>
            {businesses.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </label>
        <label className={label}>
          部門
          <select
            className={select}
            value={departmentId}
            onChange={(e) => setDepartmentId(e.target.value ? Number(e.target.value) : '')}
          >
            <option value="">すべて</option>
            {departments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </label>
        <label className={label}>
          勘定科目
          <select className={select} value={accountId} onChange={(e) => setAccountId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">すべて</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </label>
        <label className={label}>
          <input type="checkbox" checked={showPeriodBreakdown} onChange={(e) => setShowPeriodBreakdown(e.target.checked)} />
          期間ごとの内訳を表示
        </label>
      </div>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}
      {loading ? (
        <p className={mutedText}>読み込み中...</p>
      ) : rows.length === 0 ? (
        <p className={mutedText}>データがありません。</p>
      ) : (
        <div className="overflow-x-auto">
          <table className={table}>
            <thead>
              <tr>
                <th className={th}>事業</th>
                <th className={th}>部門</th>
                <th className={th}>勘定科目</th>
                <th className={th}>期間</th>
                <th className={th}>予算</th>
                <th className={th}>見込</th>
                <th className={th}>実績</th>
                <th className={th}>差異（実績-予算）</th>
              </tr>
            </thead>
            <tbody>
              {showPeriodBreakdown
                ? rows.map((r) => (
                    <tr key={`${r.business_id}-${r.department_id}-${r.account_id}-${r.period_id}`}>
                      <td className={td}>{r.business_name}</td>
                      <td className={td}>{r.department_name}</td>
                      <td className={td}>{r.account_name}</td>
                      <td className={td}>{r.period_label}</td>
                      <td className={tdRight}>{formatAmount(r.budget_amount)}</td>
                      <td className={tdRight}>{formatAmount(r.forecast_amount)}</td>
                      <td className={tdRight}>{formatAmount(r.actual_amount)}</td>
                      <td className={tdRight}>{formatAmount(r.variance_amount)}</td>
                    </tr>
                  ))
                : groups.map((g) => {
                    const expanded = expandedGroups.has(g.key)
                    return (
                      <Fragment key={g.key}>
                        <tr className="cursor-pointer" onClick={() => toggleGroup(g.key)}>
                          <td className={td}>{g.business_name}</td>
                          <td className={td}>{g.department_name}</td>
                          <td className={td}>{g.account_name}</td>
                          <td className={td}>
                            {expanded ? '▼' : '▶'} 年間合計（{g.rows.length}ヶ月）
                          </td>
                          <td className={tdRight}>{formatAmount(g.budget_amount)}</td>
                          <td className={tdRight}>{formatAmount(g.forecast_amount)}</td>
                          <td className={tdRight}>{formatAmount(g.actual_amount)}</td>
                          <td className={tdRight}>{formatAmount(g.variance_amount)}</td>
                        </tr>
                        {expanded &&
                          g.rows.map((r) => (
                            <tr key={`${g.key}-${r.period_id}`} className="bg-gray-50 dark:bg-gray-800/50">
                              <td className={td} />
                              <td className={td} />
                              <td className={td} />
                              <td className={td}>　{r.period_label}</td>
                              <td className={tdRight}>{formatAmount(r.budget_amount)}</td>
                              <td className={tdRight}>{formatAmount(r.forecast_amount)}</td>
                              <td className={tdRight}>{formatAmount(r.actual_amount)}</td>
                              <td className={tdRight}>{formatAmount(r.variance_amount)}</td>
                            </tr>
                          ))}
                      </Fragment>
                    )
                  })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
