import { lazy, Suspense, useMemo, useRef, useState, type ReactNode } from 'react'
import { download, query } from '../../api/client'
import type { Activity, List, Scenario, TreeNode, Unit } from '../../api/types'
import { Button, Card, ErrorMessage, Loading, PageHeader, Select, cx } from '../../components/ui'
import { chartViewLabels, type ChartView } from '../../lib/activityCharts'
import { saveElementAsPng } from '../../lib/domImage'
import { defaultScenarios, scenarioLabel } from '../../lib/scenario'
import { buildTree, subtreeIds } from '../../lib/tree'
import { useApi } from '../../lib/useApi'

// 図は開いたときに読み込む（I-22）
const ActivityCharts = lazy(() => import('../activities/ActivityCharts').then((m) => ({ default: m.ActivityCharts })))

type Sheet = 'pl' | 'units' | 'notes'
const sheetLabels: Record<Sheet, string> = { pl: '全社 P/L', units: 'ユニット別 P/L', notes: '変動の説明' }
const sheetHints: Record<Sheet, string> = {
  pl: '科目体系の P/L。列は期間ごとの系列と、比較元との差',
  units: 'セグメント → ユニットの売上・利益（通期）と、比較元との差',
  notes: '今回の見込の施策ごとの、目標・前回の見込との差と説明',
}
const grainLabels = { month: '月次', quarter: '四半期', half: '半期', year: '通期' }
type Grain = keyof typeof grainLabels

/** 報告資料に並べる図（docs/plan.md「2.23」） */
const packCharts: ChartView[] = ['portfolio', 'waterfall', 'pipeline']

const maxSeries = 4

/**
 * 報告資料（docs/plan.md「2.23」）。P/L と変動の説明を Excel に、主な図を画像（PNG）に出力する。
 */
export function ReportPackPage() {
  const scenarios = useApi<List<Scenario>>('/scenarios')
  const segments = useApi<List<TreeNode>>('/segments')
  const units = useApi<List<Unit>>('/units')
  const activities = useApi<List<Activity>>('/activities')

  const error = scenarios.error ?? segments.error ?? units.error ?? activities.error
  if (error) return <ErrorMessage error={error} />
  if (!scenarios.data || !segments.data || !units.data || !activities.data) return <Loading />
  return <PackView scenarios={scenarios.data.items} segments={segments.data.items} units={units.data.items} activities={activities.data.items} />
}

/** 年度の初期の系列: 目標・今回の見込・前回の見込（同じシナリオは1つにする） */
function defaultSeries(scenarios: Scenario[], fy: number): number[] {
  const inYear = scenarios.filter((s) => s.fiscal_year === fy)
  const { base, latest } = defaultScenarios(inYear)
  const current = inYear.find((s) => s.is_active) ?? latest
  const ids = [base?.id, current?.id, current?.previous_scenario_id ?? undefined].filter((id): id is number => id !== undefined)
  return [...new Set(ids)].slice(0, maxSeries)
}

function PackView({ scenarios, segments, units, activities }: { scenarios: Scenario[]; segments: TreeNode[]; units: Unit[]; activities: Activity[] }) {
  const years = [...new Set(scenarios.map((s) => s.fiscal_year))].sort((a, b) => b - a)
  const active = scenarios.find((s) => s.is_active)
  const [fy, setFy] = useState(active?.fiscal_year ?? years[0] ?? 0)
  const [series, setSeries] = useState<number[]>(() => defaultSeries(scenarios, fy))
  const [actual, setActual] = useState(true)
  const [amount, setAmount] = useState<'full' | 'weighted'>('full')
  const [grain, setGrain] = useState<Grain>('month')
  // 範囲: '' は全社、'segment:ID'・'unit:ID'
  const [scope, setScope] = useState('')
  const [sheets, setSheets] = useState<Set<Sheet>>(new Set(['pl', 'units', 'notes']))
  const [withActivities, setWithActivities] = useState(false)
  const [busy, setBusy] = useState(false)
  const [downloadError, setDownloadError] = useState<unknown>(null)

  const inYear = scenarios.filter((s) => s.fiscal_year === fy)
  const tree = useMemo(() => buildTree(segments), [segments])
  const shownUnits = units.filter((u) => !u.is_archived).sort((a, b) => a.code.localeCompare(b.code))

  const changeYear = (y: number) => {
    setFy(y)
    setSeries(defaultSeries(scenarios, y))
  }
  const setSlot = (i: number, value: string) => {
    const next = [...series]
    if (value) next[i] = Number(value)
    else next.splice(i, 1)
    setSeries([...new Set(next.filter(Boolean))])
  }
  const toggleSheet = (s: Sheet) => {
    const next = new Set(sheets)
    if (next.has(s)) next.delete(s)
    else next.add(s)
    setSheets(next)
  }

  const [scopeKind, scopeId] = scope.split(':')
  const exportExcel = async () => {
    setBusy(true)
    setDownloadError(null)
    try {
      await download(
        `/reports/pack.xlsx${query({
          scenario_ids: series.join(','),
          include_actual: actual ? 'true' : undefined,
          measure: amount === 'weighted' ? 'weighted' : undefined,
          grain,
          segment_id: scopeKind === 'segment' ? scopeId : undefined,
          unit_id: scopeKind === 'unit' ? scopeId : undefined,
          sheets: (['pl', 'units', 'notes'] as const).filter((s) => sheets.has(s)).join(','),
          activities: sheets.has('units') && withActivities ? 'true' : undefined,
        })}`,
      )
    } catch (err) {
      setDownloadError(err)
    } finally {
      setBusy(false)
    }
  }

  // 図は範囲の施策で描く
  const chartActivities = useMemo(() => {
    if (scopeKind === 'unit') return activities.filter((a) => a.unit_id === Number(scopeId))
    if (scopeKind === 'segment') {
      const ids = subtreeIds(tree, Number(scopeId))
      const unitIds = new Set(units.filter((u) => ids.has(u.segment_id)).map((u) => u.id))
      return activities.filter((a) => unitIds.has(a.unit_id))
    }
    return activities
  }, [activities, units, tree, scopeKind, scopeId])

  const chartsRef = useRef<HTMLDivElement>(null)
  const [savingAll, setSavingAll] = useState(false)
  const [saveError, setSaveError] = useState('')
  const saveAll = async () => {
    const els = Array.from(chartsRef.current?.querySelectorAll<HTMLElement>('[data-capture]') ?? [])
    setSavingAll(true)
    setSaveError('')
    try {
      for (const el of els) await saveElementAsPng(el, el.dataset.captureFile ?? el.dataset.capture ?? '図', el.dataset.capture)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : '図を画像にできませんでした')
    } finally {
      setSavingAll(false)
    }
  }

  const canExport = series.length > 0 && sheets.size > 0 && !busy

  return (
    <>
      <PageHeader
        title="報告資料"
        description="経営会議などの資料に使う P/L と変動の説明を Excel に、図を画像（PNG）に出力します。数字は予実比較と同じ集計です。閲覧制限のある科目は、見られる人にだけ含めます。"
      />

      <Card title="Excel に出力" className="mb-4">
        <div className="grid gap-3 md:grid-cols-5">
          <Control label="年度">
            <Select value={fy} onChange={(e) => changeYear(Number(e.target.value))}>
              {years.map((y) => (
                <option key={y} value={y}>
                  {y}年度
                </option>
              ))}
            </Select>
          </Control>
          {Array.from({ length: maxSeries }, (_, i) => (
            <Control key={i} label={i === 0 ? '系列 1（比較元）' : `系列 ${i + 1}`}>
              <Select value={series[i] ?? ''} onChange={(e) => setSlot(i, e.target.value)} disabled={i > series.length}>
                <option value="">（なし）</option>
                {inYear.map((s) => (
                  <option key={s.id} value={s.id} disabled={series.includes(s.id) && series[i] !== s.id}>
                    {scenarioLabel(s)}
                  </option>
                ))}
              </Select>
            </Control>
          ))}
        </div>
        <label className="mt-3 flex items-center gap-2 text-sm font-medium text-slate-700">
          <input type="checkbox" className="size-4 rounded border-slate-300" checked={actual} onChange={(e) => setActual(e.target.checked)} />
          実績を加える（取り込み済みの月）
        </label>

        <div className="mt-3 flex flex-wrap items-end gap-3 border-t border-slate-100 pt-3">
          <Segmented label="金額" value={amount} options={{ full: '満額', weighted: '加重見込' }} onChange={(v) => setAmount(v as 'full' | 'weighted')} />
          <Segmented label="期間（全社 P/L の列）" value={grain} options={grainLabels} onChange={(v) => setGrain(v as Grain)} />
          <Control label="範囲">
            <Select value={scope} onChange={(e) => setScope(e.target.value)} className="w-64">
              <option value="">全社</option>
              <optgroup label="セグメント">
                {tree.ordered.map(({ node, depth }) => (
                  <option key={node.id} value={`segment:${node.id}`}>
                    {'　'.repeat(depth)}
                    {node.name}
                  </option>
                ))}
              </optgroup>
              <optgroup label="ユニット">
                {shownUnits.map((u) => (
                  <option key={u.id} value={`unit:${u.id}`}>
                    {u.name}
                  </option>
                ))}
              </optgroup>
            </Select>
          </Control>
        </div>

        <fieldset className="mt-3 border-t border-slate-100 pt-3">
          <legend className="mb-2 text-xs font-medium text-slate-500">シート</legend>
          <div className="grid gap-2 md:grid-cols-3">
            {(['pl', 'units', 'notes'] as const).map((s) => (
              <label key={s} className="flex items-start gap-2 text-sm text-slate-700">
                <input type="checkbox" className="mt-0.5 size-4 rounded border-slate-300" checked={sheets.has(s)} onChange={() => toggleSheet(s)} />
                <span>
                  <span className="font-medium">{sheetLabels[s]}</span>
                  <span className="block text-xs text-slate-500">{sheetHints[s]}</span>
                  {s === 'units' && (
                    <span className="mt-1 flex items-center gap-1.5 text-xs text-slate-600">
                      <input
                        type="checkbox"
                        className="size-3.5 rounded border-slate-300"
                        checked={withActivities}
                        disabled={!sheets.has('units')}
                        onChange={(e) => setWithActivities(e.target.checked)}
                      />
                      施策まで出す
                    </span>
                  )}
                </span>
              </label>
            ))}
          </div>
          <p className="mt-2 text-xs text-slate-500">「条件」のシート（年度・系列・範囲・出力した日時など）は必ず付きます。</p>
        </fieldset>

        <div className="mt-4 flex flex-wrap items-center gap-3 border-t border-slate-100 pt-3">
          <Button variant="primary" onClick={exportExcel} disabled={!canExport}>
            {busy ? '出力中…' : 'Excel に出力'}
          </Button>
          {series.length === 0 && <span className="text-xs text-slate-500">系列を1つ以上選んでください</span>}
          {sheets.size === 0 && <span className="text-xs text-slate-500">シートを1つ以上選んでください</span>}
        </div>
        {downloadError !== null && (
          <div className="mt-3">
            <ErrorMessage error={downloadError} />
          </div>
        )}
      </Card>

      <Card
        title="図"
        actions={
          <>
            {saveError && (
              <span role="alert" className="text-xs text-red-600">
                {saveError}
              </span>
            )}
            <Button size="sm" onClick={saveAll} disabled={savingAll}>
              {savingAll ? '保存中…' : '図をまとめて保存'}
            </Button>
          </>
        }
      >
        <p className="mb-4 text-xs text-slate-500">今回の見込の図です（範囲で絞り込みます）。1つずつ保存するときは、図の「画像を保存」を使います。Chrome か Edge で保存できます。</p>
        <div ref={chartsRef} className="space-y-6">
          {packCharts.map((view) => (
            <section key={view} aria-label={chartViewLabels[view]}>
              <h3 className="mb-2 text-sm font-semibold text-slate-700">{chartViewLabels[view]}</h3>
              <Suspense fallback={<Loading />}>
                <ActivityCharts view={view} activities={chartActivities} />
              </Suspense>
            </section>
          ))}
        </div>
      </Card>
    </>
  )
}

function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-xs font-medium text-slate-500">
      {label}
      <div className="mt-1">{children}</div>
    </label>
  )
}

function Segmented({ label, value, options, onChange }: { label: string; value: string; options: Record<string, string>; onChange: (v: string) => void }) {
  return (
    <div>
      <div className="mb-1 text-xs font-medium text-slate-500">{label}</div>
      <div className="inline-flex rounded-md border border-slate-300 bg-white p-0.5" role="radiogroup" aria-label={label}>
        {Object.entries(options).map(([v, l]) => (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={value === v}
            onClick={() => onChange(v)}
            className={cx('rounded px-3 py-1.5 text-sm', value === v ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100')}
          >
            {l}
          </button>
        ))}
      </div>
    </div>
  )
}
