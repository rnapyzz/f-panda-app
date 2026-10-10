import { noteCauseLabels, noteStatusLabels, type ActivityProgress, type NoteCause } from '../api/types'
import { Badge, Card, cx } from '../components/ui'
import { CommentCount } from '../components/CommentCount'
import { compactAmount, summaryText, type ChangeSummary } from '../lib/home'
import { Link } from '../lib/router'
import { Help } from '../components/Help'
import { useAmountUnit } from '../lib/amountUnit'

const causeLabel = (key: NoteCause | 'multiple' | 'none') => (key === 'multiple' ? '複数の要因' : key === 'none' ? '要因なし' : noteCauseLabels[key])
const statusTone = { not_started: 'slate', in_progress: 'amber', completed: 'green' } as const
const excerpt = (s: string, n = 80) => (s.length > n ? `${s.slice(0, n)}…` : s)

/** 変動のサマリー（前回見込 → 今回）。文章・施策別（上位）・要因別 */
export function ChangeSummaryCard({ summary, scenarioId, ownerName }: { summary: ChangeSummary; scenarioId: number; ownerName: (it: ActivityProgress) => string }) {
  const { signed: signedText, unit } = useAmountUnit()
  const signed = (v: bigint) => (
    <span className={cx('tabular-nums', v > 0n && 'text-emerald-700', v < 0n && 'text-red-600', v === 0n && 'text-slate-400')}>{signedText(v)}</span>
  )
  return (
    <Card
      title={
        <>
          変動のサマリー（前回の見込 → 今回、利益・年間）
          <Help manual="manager#changes">要因が複数の施策は「複数の要因」、ない施策は「要因なし」に計上します。</Help>
        </>
      }
      className="mb-4"
    >
      {!summary.hasPrevious ? (
        <p className="text-sm text-slate-500">前回の見込が指定されていないため、変動を表示できません（シナリオ管理で指定します）。</p>
      ) : (
        <div className="space-y-4">
          <p className="rounded-md bg-slate-50 px-3 py-2 text-sm text-slate-800" aria-label="変動の文章のサマリー">
            {summaryText(summary, unit)}
            {summary.initialDiff !== null && <span className="ml-1 text-slate-500">期初計画との差は {compactAmount(summary.initialDiff, unit)}。</span>}
          </p>
          <div className="grid gap-6 lg:grid-cols-3">
            <div className="lg:col-span-2">
              <h3 className="mb-1 text-xs font-semibold text-slate-500">変動の大きい施策</h3>
              {summary.top.length === 0 ? (
                <p className="text-sm text-slate-500">前回の見込からの変動はありません。</p>
              ) : (
                <ul className="divide-y divide-slate-100" aria-label="変動の大きい施策">
                  {summary.top.map(({ item: it, diff }) => (
                    <li key={it.activity_id} className="py-2 text-sm">
                      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                        <Link to={`/activities/${it.activity_id}?tab=update&scenario=${scenarioId}`} className="font-medium text-indigo-700 hover:underline">
                          {it.name}
                        </Link>
                        {signed(diff)}
                        {it.causes.map((c) => (
                          <span key={c} className="rounded-full border border-slate-200 px-2 text-xs text-slate-600">
                            {noteCauseLabels[c]}
                          </span>
                        ))}
                        <Badge tone={statusTone[it.status]}>{noteStatusLabels[it.status]}</Badge>
                        <span className="text-xs text-slate-400">{ownerName(it)}</span>
                        <CommentCount count={it.comment_count} />
                      </div>
                      <p className={cx('mt-0.5 text-xs', it.explanation ? 'text-slate-600' : 'text-amber-700')}>{it.explanation ? excerpt(it.explanation) : '説明がありません'}</p>
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <h3 className="mb-1 text-xs font-semibold text-slate-500">要因別</h3>
              {summary.byCause.length === 0 ? (
                <p className="text-sm text-slate-500">—</p>
              ) : (
                <table aria-label="要因別の変動" className="w-full text-sm">
                  <tbody>
                    {summary.byCause.map((c) => (
                      <tr key={c.key} className="border-b border-slate-100">
                        <th scope="row" className="py-1.5 text-left font-normal text-slate-700">
                          {causeLabel(c.key)}
                          <span className="ml-1 text-xs text-slate-400">{c.count}件</span>
                        </th>
                        <td className="py-1.5 text-right">{signed(c.diff)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </div>
      )}
    </Card>
  )
}
