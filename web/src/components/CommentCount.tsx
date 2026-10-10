/** 説明へのコメントの件数（docs/plan.md「2.24」）。0 件なら何も出さない */
export function CommentCount({ count }: { count: number }) {
  if (!count) return null
  return (
    <span className="inline-flex items-center gap-0.5 text-xs text-slate-500" title={`コメント ${count}件`} aria-label={`コメント ${count}件`}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className="size-3.5" aria-hidden="true">
        <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z" />
      </svg>
      {count}
    </span>
  )
}
