// FP&A のホームの「今月の作業」（docs/plan.md「2.20」）。サイクルのチェックリストと更新の状況を組み立てる。

import type { ActivityProgress, NoteStatus } from '../api/types.ts'
import { monthLabel } from './format.ts'
import { actualThroughLabel, deadlineStatus } from './scenario.ts'

export type StepKey = 'import' | 'allocate' | 'start' | 'deadline' | 'update'
export type StepState = 'done' | 'current' | 'todo'
export type Step = { key: StepKey; label: string; detail: string; state: StepState }

type ActiveLike = { name: string; actual_through: string | null; update_deadline: string | null }

/** "2026-09" の翌月 */
export function nextMonth(ym: string): string {
  const [y, m] = ym.split('-').map(Number)
  return m === 12 ? `${y + 1}-01` : `${y}-${String(m + 1).padStart(2, '0')}`
}

/**
 * サイクルのチェックリスト。対象の月は、実績を取り込み済みの最後の月（lastMonth）。
 * 最初の未完了のステップを「今ここ」にする。すべて済んだら next に次の月の案内を返す。
 */
export function buildChecklist(input: {
  lastMonth: string | null
  unallocated: { count: number; amount: bigint }
  active: ActiveLike | null
  progress: { total: number; completed: number }
  today: string
}): { steps: Step[]; next: string | null } {
  const { lastMonth: m, unallocated, active, progress, today } = input
  const month = m ? monthLabel(m) : ''
  const raw: (Omit<Step, 'state'> & { done: boolean })[] = [
    { key: 'import', label: m ? `${month}の実績を取り込む` : '実績を取り込む', done: m !== null, detail: m ? `${month}まで取り込み済み` : 'この年度の実績はまだありません' },
    {
      key: 'allocate',
      label: '未割当をなくす',
      done: m !== null && unallocated.count === 0,
      detail: unallocated.count === 0 ? '未割当はありません' : `未割当 ${unallocated.count} 件（${unallocated.amount.toLocaleString('ja-JP')} 円）`,
    },
    {
      key: 'start',
      label: m ? `月次の見込を始める（${month}まで実績）` : '月次の見込を始める',
      done: active !== null && m !== null && (active.actual_through ?? '') >= m,
      detail: active ? `今回の見込: ${active.name}（${actualThroughLabel(active.actual_through)}）` : '今回の見込がありません',
    },
    {
      key: 'deadline',
      label: '締切を入れる',
      done: !!active?.update_deadline,
      detail: deadlineStatus(active?.update_deadline ?? null, today)?.text ?? '締切はまだありません',
    },
    {
      key: 'update',
      label: '現場の更新',
      done: active !== null && progress.completed === progress.total,
      detail: `完了 ${progress.completed} / ${progress.total} 件`,
    },
  ]
  const current = raw.findIndex((s) => !s.done)
  const steps = raw.map(({ done, ...s }, i) => ({ ...s, state: (done ? 'done' : i === current ? 'current' : 'todo') as StepState }))
  return { steps, next: current === -1 && m ? `次は${monthLabel(nextMonth(m))}の実績の取込です（会計の締めの後）` : null }
}

export type UnitProgress = { unitId: number; counts: Record<NoteStatus, number>; total: number; rate: number }

/** ユニット別の更新の状況。完了率の低い順 */
export function progressByUnit(items: ActivityProgress[]): UnitProgress[] {
  const map = new Map<number, Record<NoteStatus, number>>()
  for (const it of items) {
    const c = map.get(it.unit_id) ?? { not_started: 0, in_progress: 0, completed: 0 }
    c[it.status]++
    map.set(it.unit_id, c)
  }
  return [...map]
    .map(([unitId, counts]) => {
      const total = counts.not_started + counts.in_progress + counts.completed
      return { unitId, counts, total, rate: total === 0 ? 1 : counts.completed / total }
    })
    .sort((a, b) => a.rate - b.rate || b.total - a.total || a.unitId - b.unitId)
}

export type PendingOwner = { userId: number | null; notStarted: number; inProgress: number; lastEditedAt: string | null }

/** 未完了の施策がある担当者（userId が null は担当者なし）。未完了の多い順 */
export function pendingOwners(items: ActivityProgress[]): PendingOwner[] {
  const map = new Map<number | null, PendingOwner>()
  for (const it of items) {
    if (it.status === 'completed') continue
    const p = map.get(it.owner_user_id) ?? { userId: it.owner_user_id, notStarted: 0, inProgress: 0, lastEditedAt: null }
    if (it.status === 'not_started') p.notStarted++
    else p.inProgress++
    if (it.last_edited_at && (!p.lastEditedAt || it.last_edited_at > p.lastEditedAt)) p.lastEditedAt = it.last_edited_at
    map.set(it.owner_user_id, p)
  }
  const open = (p: PendingOwner) => p.notStarted + p.inProgress
  return [...map.values()].sort((a, b) => Number(a.userId === null) - Number(b.userId === null) || open(b) - open(a) || (a.userId ?? 0) - (b.userId ?? 0))
}
