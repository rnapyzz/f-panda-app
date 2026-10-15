// 予実比較 API の施策ごとの行を、対象のユニットごとに取得して覚えておく（ダッシュボード・シナリオの全体像）。
// 掘り下げ・絞り込みで対象のユニットが変わっても、取得済みのユニットは取り直さない。

import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { ComparisonReport, ReportRow } from '../api/types'

/** 対象のユニットの、施策ごとの予実比較の行（ユニットごとに取得して覚えておく） */
export function useActivityRows(reportQuery: string | null, unitIds: number[]) {
  const [cache, setCache] = useState<Map<string, ReportRow[]>>(new Map())
  const [error, setError] = useState<unknown>(null)
  const missing = reportQuery ? unitIds.filter((id) => !cache.has(`${reportQuery}|${id}`)) : []
  const missingKey = missing.join(',')
  useEffect(() => {
    if (!reportQuery || missingKey === '') return
    let cancelled = false
    Promise.all(
      missingKey.split(',').map(async (id) => [id, (await api.get<ComparisonReport>(`/reports/comparison${reportQuery}&unit_id=${id}`)).rows] as const),
    )
      .then((results) => {
        if (cancelled) return
        setCache((prev) => {
          const next = new Map(prev)
          for (const [id, rows] of results) next.set(`${reportQuery}|${id}`, rows)
          return next
        })
      })
      .catch((err) => !cancelled && setError(err))
    return () => {
      cancelled = true
    }
  }, [reportQuery, missingKey])
  const rows = reportQuery && missing.length === 0 ? unitIds.flatMap((id) => cache.get(`${reportQuery}|${id}`) ?? []) : null
  return { rows, error }
}
