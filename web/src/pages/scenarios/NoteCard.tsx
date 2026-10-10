import { useState } from 'react'
import { api } from '../../api/client'
import { noteCauseLabels, noteStatusLabels, type AmountRow, type NoteCause, type ValuesView } from '../../api/types'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { CommentThread } from './CommentThread'
import { Badge, Button, Card, ErrorMessage, Textarea, cx } from '../../components/ui'
import { Help } from '../../components/Help'
import { formatDateTime, formatYen } from '../../lib/format'
import { isLargeVariance, summarizeVariance, type VarianceSummary } from '../../lib/variance'

const statusTone = { not_started: 'slate', in_progress: 'amber', completed: 'green' } as const

/**
 * 今回の見込の説明と更新の完了（docs/plan.md「2.10」の ⑤⑥、「2.18」）。
 * 左に自動の差異サマリー（目標・前回の見込との年間の差、差の大きい科目・月）、右に説明欄と「説明して完了」を置く。
 */
export function NoteCard({
  path,
  view,
  editable,
  dirty,
  months,
  current,
  base,
  previous,
  onSaved,
}: {
  path: string
  view: ValuesView
  editable: boolean
  /** 数値に未保存の変更があるか（あれば完了にできない） */
  dirty: boolean
  months: string[]
  current: AmountRow[]
  base?: AmountRow[]
  previous?: AmountRow[]
  onSaved: (v: ValuesView) => void
}) {
  const note = view.note
  const [explanation, setExplanation] = useState(note.explanation)
  const [causes, setCauses] = useState<NoteCause[]>(note.causes)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  // サーバーの値が変わったら（保存・再読み込み）、入力欄も合わせる
  const [synced, setSynced] = useState(note)
  if (synced !== note) {
    setSynced(note)
    setExplanation(note.explanation)
    setCauses(note.causes)
  }

  const noteChanged = explanation.trim() !== note.explanation || causes.join(',') !== note.causes.join(',')
  const vsBase = base ? summarizeVariance(current, base, months) : null
  const vsPrevious = previous ? summarizeVariance(current, previous, months) : null
  const completed = note.status === 'completed'

  const run = async (op: () => Promise<ValuesView>) => {
    setBusy(true)
    setError(null)
    try {
      onSaved(await op())
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }
  const saveNote = () => api.put<ValuesView>(`${path}/note`, { explanation, causes })
  const complete = () =>
    run(async () => {
      if (noteChanged) await saveNote()
      return api.post<ValuesView>(`${path}/complete`, {})
    })
  const requestComplete = () => {
    // 説明がないまま、目標との差が大きいときは確認する（完了は止めない）
    if (!explanation.trim() && vsBase && isLargeVariance(vsBase.diff, vsBase.compare)) setConfirming(true)
    else complete()
  }

  return (
    <Card
      title={
        <>
          今回の見込の説明
          <Help manual="member#note">
            目標・前回の見込との差がなぜ生じたか、想定している条件（楽観・悲観の見通しなど）を書きます。「説明して完了」で、この施策の今回の更新が終わったことを記録します。完了の後に数値や説明を変えると「入力中」に戻ります。下のコメントで、マネージャーなどとやり取りできます（コメントでは状態は変わりません）。
          </Help>
        </>
      }
      className="mb-4"
      actions={
        <span className="flex items-center gap-2 text-xs text-slate-500" role="status" aria-label="更新の状態">
          <Badge tone={statusTone[note.status]}>{noteStatusLabels[note.status]}</Badge>
          {completed && note.completed_at && (
            <span>
              {formatDateTime(note.completed_at)} {note.completed_by_name}
            </span>
          )}
        </span>
      }
    >
      <div className="grid gap-6 lg:grid-cols-2">
        <div className="space-y-4 text-sm">
          <Summary title="目標との差（年間の利益）" summary={vsBase} empty="目標のシナリオがありません" />
          <Summary title="前回の見込との差（年間の利益）" summary={vsPrevious} empty="前回の見込が指定されていません" />
        </div>

        <div className="space-y-3">
          <fieldset disabled={!editable || busy}>
            <legend className="mb-1 text-xs font-medium text-slate-500">要因の分類（任意・複数選択）</legend>
            <div className="flex flex-wrap gap-1.5">
              {(Object.keys(noteCauseLabels) as NoteCause[]).map((c) => {
                const on = causes.includes(c)
                return (
                  <label
                    key={c}
                    className={cx(
                      'cursor-pointer rounded-full border px-2.5 py-1 text-xs select-none',
                      on ? 'border-indigo-400 bg-indigo-50 text-indigo-800' : 'border-slate-200 text-slate-600 hover:bg-slate-50',
                      !editable && 'cursor-default',
                    )}
                  >
                    <input
                      type="checkbox"
                      className="sr-only"
                      checked={on}
                      onChange={() => setCauses(on ? causes.filter((x) => x !== c) : (Object.keys(noteCauseLabels) as NoteCause[]).filter((x) => x === c || causes.includes(x)))}
                    />
                    {noteCauseLabels[c]}
                  </label>
                )
              })}
            </div>
          </fieldset>
          <Textarea
            aria-label="今回の見込の説明"
            value={explanation}
            onChange={(e) => setExplanation(e.target.value)}
            disabled={!editable || busy}
            placeholder="例: A社の受注が 10月から 12月にずれたため、Q3 の売上が目標を下回る。年間では前回の見込どおり。B社の追加発注（2件）が決まれば上振れ"
            className="min-h-28"
          />
          {editable ? (
            <div className="flex flex-wrap items-center gap-2">
              {completed ? (
                <>
                  <Button disabled={!noteChanged || busy} onClick={() => run(saveNote)}>
                    説明を保存
                  </Button>
                  <Button disabled={busy} onClick={() => run(() => api.del<ValuesView>(`${path}/complete`, {}))}>
                    完了を取り消す
                  </Button>
                </>
              ) : (
                <>
                  <Button variant="primary" disabled={dirty || busy} onClick={requestComplete}>
                    説明して完了
                  </Button>
                  <Button disabled={!noteChanged || busy} onClick={() => run(saveNote)}>
                    下書き保存
                  </Button>
                </>
              )}
              {dirty && !completed && <span className="text-xs text-amber-700">数値の変更を保存してから完了にしてください</span>}
            </div>
          ) : (
            !note.explanation && <p className="text-xs text-slate-400">説明はまだありません</p>
          )}
          {error ? <ErrorMessage error={error} /> : null}
        </div>
      </div>

      <div className="mt-6 border-t border-slate-100 pt-4">
        <CommentThread key={path} path={path} />
      </div>

      <ConfirmDialog
        open={confirming}
        title="説明なしで完了にする"
        message={<>目標との差が大きい（年間の利益で {formatYen(String(vsBase?.diff ?? 0n))} 円）のに、説明が書かれていません。このまま完了にしますか？</>}
        confirmLabel="完了にする"
        onClose={() => setConfirming(false)}
        onConfirm={async () => {
          await complete()
        }}
      />
    </Card>
  )
}

function Summary({ title, summary, empty }: { title: string; summary: VarianceSummary | null; empty: string }) {
  const signed = (x: bigint) => `${x > 0n ? '+' : ''}${formatYen(String(x))}`
  const tone = (x: bigint) => (x > 0n ? 'text-emerald-700' : x < 0n ? 'text-red-600' : 'text-slate-500')
  return (
    <div>
      <h3 className="mb-1 text-xs font-semibold text-slate-500">{title}</h3>
      {!summary ? (
        <p className="text-xs text-slate-400">{empty}</p>
      ) : (
        <>
          <p className="tabular-nums">
            <span className={cx('text-base font-semibold', tone(summary.diff))}>{signed(summary.diff)}</span>
            <span className="ml-2 text-xs text-slate-500">
              （今回 {formatYen(String(summary.current))} / 比較 {formatYen(String(summary.compare))}）
            </span>
          </p>
          {summary.subjects.length > 0 && (
            <p className="mt-1 text-xs text-slate-600">
              差の大きい科目:{' '}
              {summary.subjects.map((s, i) => (
                <span key={s.label}>
                  {i > 0 && '、'}
                  {s.label} <span className="tabular-nums">{signed(s.diff)}</span>
                </span>
              ))}
            </p>
          )}
          {summary.months.length > 0 && (
            <p className="mt-0.5 text-xs text-slate-600">
              差の大きい月（利益）:{' '}
              {summary.months.map((m, i) => (
                <span key={m.month}>
                  {i > 0 && '、'}
                  {m.label} <span className="tabular-nums">{signed(m.diff)}</span>
                </span>
              ))}
            </p>
          )}
        </>
      )}
    </div>
  )
}
