import { useState } from 'react'
import { api } from '../../api/client'
import type { NoteCommentList } from '../../api/types'
import { Button, ErrorMessage, Loading, Textarea } from '../../components/ui'
import { formatDateTime } from '../../lib/format'
import { useApi } from '../../lib/useApi'

const maxLength = 1000

/**
 * 今回の見込の説明へのコメント（docs/plan.md「2.24」）。施策 × シナリオのやり取りを書いた順に並べる。
 * 施策を見られる人はだれでも書ける。書いた本人は編集・削除できる（編集すると「編集済み」と出る）。ロック済みのシナリオでは読むだけ。path は /scenarios/{id}/activities/{aid}。
 */
export function CommentThread({ path }: { path: string }) {
  const comments = useApi<NoteCommentList>(`${path}/comments`)
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  // 編集中のコメント（id と本文）
  const [editing, setEditing] = useState<{ id: number; body: string } | null>(null)

  const run = async (op: () => Promise<NoteCommentList>, after?: () => void) => {
    setBusy(true)
    setError(null)
    try {
      comments.setData(await op())
      after?.()
    } catch (err) {
      setError(err)
    } finally {
      setBusy(false)
    }
  }

  if (comments.error) return <ErrorMessage error={comments.error} />
  if (!comments.data) return <Loading />
  const { items, can_post } = comments.data
  const length = [...body.trim()].length

  return (
    <section aria-label="コメント">
      <h3 className="mb-2 text-xs font-semibold text-slate-500">コメント{items.length > 0 && `（${items.filter((c) => !c.deleted).length}）`}</h3>
      {items.length === 0 ? (
        <p className="mb-3 text-xs text-slate-400">コメントはまだありません。気になる点があれば書いてください（担当者にお知らせが届きます）。</p>
      ) : (
        <ol className="mb-3 space-y-2">
          {items.map((c) => (
            <li key={c.id} className="rounded-md bg-slate-50 px-3 py-2 text-sm">
              <div className="flex items-center gap-2 text-xs text-slate-500">
                <span className="font-medium text-slate-700">{c.user_name}</span>
                <span>{formatDateTime(c.created_at)}</span>
                {c.edited_at && <span title={`${formatDateTime(c.edited_at)} に編集`}>（編集済み）</span>}
                {editing?.id !== c.id && (c.can_edit || c.can_delete) && (
                  <span className="ml-auto flex gap-3">
                    {c.can_edit && (
                      <button type="button" className="text-slate-400 hover:text-indigo-700" disabled={busy} onClick={() => setEditing({ id: c.id, body: c.body })}>
                        編集
                      </button>
                    )}
                    {c.can_delete && (
                      <button
                        type="button"
                        className="text-slate-400 hover:text-red-600"
                        disabled={busy}
                        onClick={() => run(() => api.del<NoteCommentList>(`${path}/comments/${c.id}`))}
                      >
                        削除
                      </button>
                    )}
                  </span>
                )}
              </div>
              {c.deleted ? (
                <p className="mt-0.5 text-xs text-slate-400 italic">このコメントは削除されました</p>
              ) : editing?.id === c.id ? (
                <div className="mt-1 space-y-2">
                  <Textarea aria-label="コメントを編集" value={editing.body} onChange={(e) => setEditing({ id: c.id, body: e.target.value })} disabled={busy} className="min-h-16" />
                  <div className="flex items-center gap-2">
                    <Button
                      size="sm"
                      variant="primary"
                      disabled={busy || [...editing.body.trim()].length === 0 || [...editing.body.trim()].length > maxLength}
                      onClick={() => run(() => api.put<NoteCommentList>(`${path}/comments/${c.id}`, { body: editing.body }), () => setEditing(null))}
                    >
                      保存
                    </Button>
                    <Button size="sm" disabled={busy} onClick={() => setEditing(null)}>
                      キャンセル
                    </Button>
                    <span className={[...editing.body.trim()].length > maxLength ? 'text-xs text-red-600' : 'text-xs text-slate-400'}>
                      {[...editing.body.trim()].length} / {maxLength} 文字
                    </span>
                  </div>
                </div>
              ) : (
                <p className="mt-0.5 whitespace-pre-wrap text-slate-800">{c.body}</p>
              )}
            </li>
          ))}
        </ol>
      )}
      {can_post ? (
        <div className="space-y-2">
          <Textarea aria-label="コメントを書く" value={body} onChange={(e) => setBody(e.target.value)} disabled={busy} placeholder="コメントを書く" className="min-h-16" />
          <div className="flex items-center gap-3">
            <Button size="sm" disabled={busy || length === 0 || length > maxLength} onClick={() => run(() => api.post<NoteCommentList>(`${path}/comments`, { body }), () => setBody(''))}>
              コメントする
            </Button>
            <span className={length > maxLength ? 'text-xs text-red-600' : 'text-xs text-slate-400'}>
              {length} / {maxLength} 文字
            </span>
          </div>
        </div>
      ) : (
        <p className="text-xs text-slate-400">ロックされたシナリオには、コメントを書けません。</p>
      )}
      {error ? <ErrorMessage error={error} /> : null}
    </section>
  )
}
