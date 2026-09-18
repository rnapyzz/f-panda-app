import { useCallback, useEffect, useState } from 'react'
import { scenarioVersionsApi, type ScenarioType, type ScenarioVersion } from '../api/scenarios'

export interface UseScenarioVersionsResult {
  versions: ScenarioVersion[]
  versionId: number | ''
  setVersionId: (id: number | '') => void
  loading: boolean
  error: string | null
  reload: () => Promise<void>
}

// Shared by every page that needs to pick a scenario version to work
// against (import, sheet submission, version management): fetches the
// versions for (scenarioType, fiscalYear) and auto-selects the current one.
export function useScenarioVersions(scenarioType: ScenarioType, fiscalYear: number): UseScenarioVersionsResult {
  const [versions, setVersions] = useState<ScenarioVersion[]>([])
  const [versionId, setVersionId] = useState<number | ''>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const list = await scenarioVersionsApi.list(scenarioType, fiscalYear)
      setVersions(list)
      const current = list.find((v) => v.is_current)
      setVersionId(current?.id ?? '')
      setError(null)
    } catch {
      setError('バージョン一覧の取得に失敗しました')
    } finally {
      setLoading(false)
    }
  }, [scenarioType, fiscalYear])

  useEffect(() => {
    void reload()
  }, [reload])

  return { versions, versionId, setVersionId, loading, error, reload }
}
