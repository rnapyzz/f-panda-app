// 利益の振れ幅の帯（悲観 ― 加重見込 ― 楽観。docs/plan.md「2.8」）。リスク画面とダッシュボードで使う。

import { cx } from './ui'
import { useAmountUnit } from '../lib/amountUnit'

/** 悲観 ― 加重見込 ― 楽観 を1本の帯で表す。実績で確定した分は濃いグレーで示す */
export function ProfitBand({ pessimistic, weighted, optimistic, actual }: { pessimistic: bigint; weighted: bigint; optimistic: bigint; actual: bigint | null }) {
  const { fmt: yen } = useAmountUnit()
  const values = [pessimistic, weighted, optimistic, 0n, ...(actual === null ? [] : [actual])]
  const min = values.reduce((a, b) => (b < a ? b : a))
  const max = values.reduce((a, b) => (b > a ? b : a))
  const span = max - min || 1n
  const pos = (v: bigint) => Number(((v - min) * 1000n) / span) / 10
  // 加重見込のラベルは帯の上、悲観・楽観は帯の下に出す。端に近いラベルは内側に寄せる。
  // 悲観と楽観が近いときは、2つのラベルを1つにまとめて重ならないようにする
  const near = pos(optimistic) - pos(pessimistic) < 16
  const edge = (p: number): 'center' | 'left' | 'right' => (p > 80 ? 'right' : p < 20 ? 'left' : 'center')
  const rangeEdge = edge((pos(pessimistic) + pos(optimistic)) / 2)
  const rangeAt = rangeEdge === 'right' ? pos(optimistic) : rangeEdge === 'left' ? pos(pessimistic) : (pos(pessimistic) + pos(optimistic)) / 2
  const marker = (v: bigint, label: string, opts: { strong?: boolean; above?: boolean; hideLabel?: boolean } = {}) => {
    const { strong = false, above = false, hideLabel = false } = opts
    const align = edge(pos(v))
    return (
      <>
        <div className={cx('absolute top-6 h-7 w-0.5 -translate-x-1/2', strong ? 'bg-slate-900' : 'bg-slate-500')} style={{ left: `${pos(v)}%` }} />
        {!hideLabel && (
        <div
          className={cx(
            'absolute text-xs whitespace-nowrap',
            above ? 'top-0' : 'top-14',
            align === 'center' && '-translate-x-1/2 text-center',
            align === 'right' && '-translate-x-full text-right',
            align === 'left' && 'text-left',
          )}
          style={{ left: `${pos(v)}%` }}
        >
          {above ? (
            <span className={cx(strong ? 'font-semibold text-slate-900' : 'text-slate-700')}>
              {label} <span className="tabular-nums">{yen(v)}</span>
            </span>
          ) : (
            <>
              <div className="text-slate-500">{label}</div>
              <div className="text-slate-700 tabular-nums">{yen(v)}</div>
            </>
          )}
        </div>
        )}
      </>
    )
  }
  return (
    <div role="img" aria-label={`利益: 悲観 ${yen(pessimistic)}、加重見込 ${yen(weighted)}、楽観 ${yen(optimistic)}${actual === null ? '' : `、うち実績 ${yen(actual)}`}`} className="px-10 pb-2">
      <div className="relative h-28">
        <div className="absolute top-8 right-0 left-0 h-3 rounded-full bg-slate-100" />
        <div
          className="absolute top-8 h-3 rounded-full bg-indigo-200"
          style={{ left: `${pos(pessimistic)}%`, width: `${Math.max(pos(optimistic) - pos(pessimistic), 0.5)}%` }}
          title={`振れ幅 ${yen(optimistic - pessimistic)}`}
        />
        {actual !== null && actual !== 0n && (
          <div
            className="absolute top-8 h-3 rounded-full bg-slate-500"
            style={{ left: `${Math.min(pos(0n), pos(actual))}%`, width: `${Math.abs(pos(actual) - pos(0n))}%` }}
            title={`実績で確定した分 ${yen(actual)}`}
          />
        )}
        {marker(pessimistic, '悲観', { hideLabel: near })}
        {marker(weighted, '加重見込', { strong: true, above: true })}
        {marker(optimistic, '楽観', { hideLabel: near })}
        {near && (
          <div
            className={cx(
              'absolute top-14 text-xs whitespace-nowrap text-slate-700',
              rangeEdge === 'center' && '-translate-x-1/2',
              rangeEdge === 'right' && '-translate-x-full',
            )}
            style={{ left: `${rangeAt}%` }}
          >
            <span className="text-slate-500">悲観</span> <span className="tabular-nums">{yen(pessimistic)}</span> 〜 <span className="text-slate-500">楽観</span>{' '}
            <span className="tabular-nums">{yen(optimistic)}</span>
          </div>
        )}
      </div>
      {actual !== null && actual !== 0n && (
        <p className="mt-1 text-xs text-slate-500">
          <span className="mr-1 inline-block size-2.5 rounded-full bg-slate-500 align-middle" />
          実績で確定した分 {yen(actual)}（振れ幅は残りの月から生じます）
        </p>
      )}
    </div>
  )
}
