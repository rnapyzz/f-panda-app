import { useState, type RefObject } from 'react'
import { saveElementAsPng } from '../lib/domImage'
import { Button } from './ui'

/**
 * 図を PNG で保存するボタン（docs/plan.md「2.23」）。target の要素を画像にする。ボタン自体は画像に含めない。
 * 保存できなかったときは、ボタンの横に理由を出す。
 */
export function SaveImageButton({ target, fileName, title }: { target: RefObject<HTMLElement | null>; fileName: string; title?: string }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async () => {
    if (!target.current) return
    setBusy(true)
    setError('')
    try {
      await saveElementAsPng(target.current, fileName, title)
    } catch (err) {
      setError(err instanceof Error ? err.message : '図を画像にできませんでした')
    } finally {
      setBusy(false)
    }
  }
  return (
    <span data-no-capture className="inline-flex items-center gap-2">
      {error && (
        <span role="alert" className="text-xs text-red-600">
          {error}
        </span>
      )}
      <Button size="sm" onClick={save} disabled={busy}>
        {busy ? '保存中…' : '画像を保存'}
      </Button>
    </span>
  )
}
