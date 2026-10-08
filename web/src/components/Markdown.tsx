import type { ReactNode } from 'react'
import { Link } from '../lib/router'
import type { Block, Inline } from '../lib/markdown'
import { Table } from './ui'

function inline(nodes: Inline[]): ReactNode[] {
  return nodes.map((n, i) => {
    switch (n.t) {
      case 'text':
        return n.v
      case 'strong':
        return <strong key={i}>{inline(n.c)}</strong>
      case 'code':
        return (
          <code key={i} className="rounded bg-slate-100 px-1 font-mono text-[0.9em] text-slate-800">
            {n.v}
          </code>
        )
      case 'link':
        if (n.href.startsWith('#'))
          return (
            <a key={i} href={n.href} className="text-indigo-700 underline">
              {inline(n.c)}
            </a>
          )
        if (n.href.startsWith('/'))
          return (
            <Link key={i} to={n.href} className="text-indigo-700 underline">
              {inline(n.c)}
            </Link>
          )
        return (
          <a key={i} href={n.href} target="_blank" rel="noreferrer" className="text-indigo-700 underline">
            {inline(n.c)}
          </a>
        )
    }
  })
}

/** 手引きの Markdown（lib/markdown.ts で変換したもの）を表示する */
export function Markdown({ blocks }: { blocks: Block[] }) {
  return (
    <div className="space-y-4 text-sm leading-relaxed text-slate-700">
      {blocks.map((b, i) => {
        switch (b.t) {
          case 'title':
            return (
              <h1 key={i} className="text-xl font-semibold text-slate-900">
                {inline(b.c)}
              </h1>
            )
          case 'h':
            return b.level === 2 ? (
              <h2 key={i} id={b.id} className="scroll-mt-4 border-b border-slate-200 pt-4 pb-1 text-base font-semibold text-slate-900">
                {inline(b.c)}
              </h2>
            ) : (
              <h3 key={i} id={b.id} className="scroll-mt-4 pt-2 font-semibold text-slate-800">
                {inline(b.c)}
              </h3>
            )
          case 'p':
            return <p key={i}>{inline(b.c)}</p>
          case 'ul':
            return (
              <ul key={i} className="list-disc space-y-1 pl-5">
                {b.items.map((it, j) => (
                  <li key={j}>{inline(it)}</li>
                ))}
              </ul>
            )
          case 'ol':
            return (
              <ol key={i} className="list-decimal space-y-1 pl-5">
                {b.items.map((it, j) => (
                  <li key={j}>{inline(it)}</li>
                ))}
              </ol>
            )
          case 'table':
            return (
              <div key={i} className="overflow-x-auto rounded border border-slate-200">
                <Table>
                  <thead>
                    <tr>
                      {b.head.map((c, j) => (
                        <th key={j}>{inline(c)}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {b.rows.map((r, j) => (
                      <tr key={j}>
                        {r.map((c, k) => (
                          <td key={k} className="align-top">
                            {inline(c)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
            )
          case 'note':
            return (
              <p key={i} className="rounded-md border-l-4 border-indigo-300 bg-indigo-50/60 px-3 py-2 text-slate-700">
                {inline(b.c)}
              </p>
            )
          case 'img':
            return (
              <figure key={i} className="space-y-1">
                <img src={b.src} alt={b.alt} loading="lazy" className="w-full rounded-md border border-slate-200 shadow-sm" />
                <figcaption className="text-xs text-slate-500">{b.alt}</figcaption>
              </figure>
            )
        }
      })}
    </div>
  )
}
