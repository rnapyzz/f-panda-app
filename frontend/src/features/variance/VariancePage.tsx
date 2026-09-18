import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { accountsApi, businessesApi, departmentsApi, type Account, type Business, type Department } from '../../api/dimensions'
import { factsApi, type VarianceRow } from '../../api/facts'

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
      })
      .catch(() => setError('予実差異レポートの取得に失敗しました'))
      .finally(() => setLoading(false))
  }, [fiscalYear, businessId, departmentId, accountId])

  return (
    <div style={{ maxWidth: 960, margin: '40px auto' }}>
      <p>
        <Link to="/">← ダッシュボード</Link>
      </p>
      <h1>予実差異レポート</h1>

      <div style={{ marginBottom: 16 }}>
        <label>
          会計年度{' '}
          <input
            type="number"
            value={fiscalYear}
            onChange={(e) => setFiscalYear(Number(e.target.value))}
            style={{ width: 80 }}
          />
        </label>{' '}
        <label>
          事業{' '}
          <select value={businessId} onChange={(e) => setBusinessId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">すべて</option>
            {businesses.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </label>{' '}
        <label>
          部門{' '}
          <select
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
        </label>{' '}
        <label>
          勘定科目{' '}
          <select value={accountId} onChange={(e) => setAccountId(e.target.value ? Number(e.target.value) : '')}>
            <option value="">すべて</option>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error && <p role="alert">{error}</p>}
      {loading ? (
        <p>読み込み中...</p>
      ) : rows.length === 0 ? (
        <p>データがありません。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>事業</th>
              <th>部門</th>
              <th>勘定科目</th>
              <th>期間</th>
              <th style={{ textAlign: 'right' }}>予算</th>
              <th style={{ textAlign: 'right' }}>見込</th>
              <th style={{ textAlign: 'right' }}>実績</th>
              <th style={{ textAlign: 'right' }}>差異（実績-予算）</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={`${r.business_id}-${r.department_id}-${r.account_id}-${r.period_id}`}>
                <td>{r.business_name}</td>
                <td>{r.department_name}</td>
                <td>{r.account_name}</td>
                <td>{r.period_label}</td>
                <td style={{ textAlign: 'right' }}>{formatAmount(r.budget_amount)}</td>
                <td style={{ textAlign: 'right' }}>{formatAmount(r.forecast_amount)}</td>
                <td style={{ textAlign: 'right' }}>{formatAmount(r.actual_amount)}</td>
                <td style={{ textAlign: 'right' }}>{formatAmount(r.variance_amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
