import { useEffect, useMemo } from 'react'
import type { Role } from '../../api/types'
import { Markdown } from '../../components/Markdown'
import { Card, PageHeader, cx } from '../../components/ui'
import { useCurrentUser } from '../../lib/auth'
import { parseMarkdown, toc } from '../../lib/markdown'
import { Link, Redirect } from '../../lib/router'
import intro from '../../manual/intro.md?raw'
import member from '../../manual/member.md?raw'
import manager from '../../manual/manager.md?raw'
import fpa from '../../manual/fpa.md?raw'

/** 手引き（docs/plan.md「2.21」）。本文は src/manual/*.md */
const manuals = [
  { key: 'intro', title: 'はじめに', src: intro },
  { key: 'member', title: '現場担当', src: member },
  { key: 'manager', title: 'マネージャー', src: manager },
  { key: 'fpa', title: 'FP&A', src: fpa },
] as const

const defaultManual: Record<Role, string> = { member: 'member', manager: 'manager', fpa_admin: 'fpa', viewer: 'intro' }

/** 使い方（/manual/:page）。左に手引きと目次、右に本文 */
export function ManualPage({ page }: { page?: string }) {
  const me = useCurrentUser()
  const current = manuals.find((m) => m.key === page)
  const blocks = useMemo(() => (current ? parseMarkdown(current.src) : []), [current])
  // 節（#id）を指定して開いたときは、その見出しまで移動する
  useEffect(() => {
    const id = decodeURIComponent(window.location.hash.slice(1))
    if (id) document.getElementById(id)?.scrollIntoView()
  }, [current])

  if (!current) return <Redirect to={`/manual/${defaultManual[me.role]}`} />
  return (
    <>
      <PageHeader title="使い方" description="このアプリの使い方です。ロールごとの手引きを選んでください。" />
      <div className="grid gap-4 lg:grid-cols-[14rem_1fr]">
        <nav aria-label="手引き" className="space-y-4 lg:sticky lg:top-4 lg:self-start">
          <ul className="space-y-0.5">
            {manuals.map((m) => (
              <li key={m.key}>
                <Link
                  to={`/manual/${m.key}`}
                  aria-current={m.key === current.key ? 'page' : undefined}
                  className={cx('block rounded-md px-3 py-1.5 text-sm font-medium', m.key === current.key ? 'bg-indigo-50 text-indigo-700' : 'text-slate-600 hover:bg-slate-100')}
                >
                  {m.title}
                  {m.key === defaultManual[me.role] && m.key !== 'intro' && <span className="ml-1 text-xs font-normal text-slate-400">（あなた向け）</span>}
                </Link>
              </li>
            ))}
          </ul>
          <ul className="space-y-0.5 border-t border-slate-200 pt-3 text-xs" aria-label="目次">
            {toc(blocks).map((h) => (
              <li key={h.id} className={h.level === 3 ? 'pl-3' : ''}>
                <a href={`#${h.id}`} className="block rounded px-3 py-1 text-slate-500 hover:bg-slate-100 hover:text-slate-800">
                  {h.text}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <Card>
          <article aria-label={`${current.title}の手引き`}>
            <Markdown blocks={blocks} />
          </article>
        </Card>
      </div>
    </>
  )
}
