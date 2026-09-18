import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { sheetsApi, type SheetSummary } from '../../api/inputsheet'
import { buttonPrimary, errorText, input, link, mutedText, pageHeading } from '../../lib/ui'

// A blank Univer workbook snapshot, used when creating a new sheet.
const BLANK_SNAPSHOT = { sheets: {}, sheetOrder: [] }

export function SheetsListPage() {
  const navigate = useNavigate()
  const [sheets, setSheets] = useState<SheetSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    sheetsApi
      .list()
      .then(setSheets)
      .catch(() => setError('シート一覧の取得に失敗しました'))
      .finally(() => setLoading(false))
  }, [])

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      const { id } = await sheetsApi.create(newName, BLANK_SNAPSHOT)
      navigate(`/sheets/${id}`)
    } catch {
      setError('シートの作成に失敗しました')
    }
  }

  return (
    <div className="mx-auto mt-10 max-w-xl px-4">
      <p className="mb-4">
        <Link to="/" className={link}>
          ← ダッシュボード
        </Link>
      </p>
      <h1 className={pageHeading}>マイシート</h1>
      <p className={`${mutedText} mb-4`}>
        自分の使いやすいレイアウトで自由に作成し、必要な範囲だけを予算・見込・実績の項目に対応付けて提出できます。
      </p>

      {error && (
        <p role="alert" className={errorText}>
          {error}
        </p>
      )}

      {loading ? (
        <p className={mutedText}>読み込み中...</p>
      ) : sheets.length === 0 ? (
        <p className={mutedText}>まだシートがありません。</p>
      ) : (
        <ul className="mb-6 space-y-1">
          {sheets.map((s) => (
            <li key={s.id}>
              <Link to={`/sheets/${s.id}`} className={link}>
                {s.name}
              </Link>
            </li>
          ))}
        </ul>
      )}

      <form onSubmit={handleCreate} className="flex items-center gap-2">
        <input
          className={input}
          placeholder="新しいシート名"
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          required
        />
        <button type="submit" className={buttonPrimary}>
          作成
        </button>
      </form>
    </div>
  )
}
