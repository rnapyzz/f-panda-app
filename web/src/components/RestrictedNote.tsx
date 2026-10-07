/** 閲覧制限のある科目を除いた金額であることの表示（docs/plan.md「2.17」）。restricted_hidden のときだけ出す */
export function RestrictedNote({ hidden, className = '' }: { hidden: boolean | undefined; className?: string }) {
  if (!hidden) return null
  return (
    <p className={`rounded-md bg-slate-100 px-3 py-1.5 text-xs text-slate-600 ${className}`} role="note">
      閲覧制限のある科目を除いた金額です。
    </p>
  )
}
