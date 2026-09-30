// API クライアント。すべての API 呼び出しはここを通す。
// エラーレスポンス {"error": {code, message, details, rows}} は ApiError として投げる。

export type RowError = { line: number; message: string }

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: Record<string, string>
  readonly rows: RowError[]

  constructor(status: number, code: string, message: string, details?: Record<string, string>, rows?: RowError[]) {
    super(message)
    this.status = status
    this.code = code
    this.details = details ?? {}
    this.rows = rows ?? []
  }

  /** 変更理由の入力が必要というエラーか */
  get needsReason(): boolean {
    return this.status === 422 && 'reason' in this.details
  }
}

type UnauthorizedListener = () => void
const unauthorizedListeners = new Set<UnauthorizedListener>()

/** 401（未ログイン・セッション切れ）を受け取ったときに呼ばれる関数を登録する */
export function onUnauthorized(fn: UnauthorizedListener): () => void {
  unauthorizedListeners.add(fn)
  return () => unauthorizedListeners.delete(fn)
}

/** エラーレスポンスを ApiError にする。401 なら未ログインの通知も行う */
async function toApiError(res: Response, path: string): Promise<ApiError> {
  const text = await res.text()
  let e: { code?: string; message?: string; details?: Record<string, string>; rows?: RowError[] } = {}
  try {
    e = text ? (JSON.parse(text).error ?? {}) : {}
  } catch {
    // JSON 以外（プロキシのエラー画面など）
  }
  if (res.status === 401 && path !== '/auth/login') {
    unauthorizedListeners.forEach((fn) => fn())
  }
  return new ApiError(res.status, e.code ?? 'unknown', e.message ?? `エラーが発生しました（${res.status}）`, e.details, e.rows)
}

/**
 * ファイルをダウンロードして保存する（CSV のエクスポートなど）。
 * 失敗したときは ApiError を投げる（ブラウザの「ダウンロードできませんでした」ではなく、理由を画面に出すため）。
 */
export async function download(path: string): Promise<void> {
  const res = await fetch(`/api${path}`, { credentials: 'same-origin' })
  if (!res.ok) throw await toApiError(res, path)
  const blob = await res.blob()
  const match = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = match?.[1] ?? 'download'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

async function request<T>(method: string, path: string, body?: unknown, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {}
  let payload: BodyInit | undefined
  if (body instanceof FormData) {
    payload = body
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }

  const res = await fetch(`/api${path}`, { method, headers, body: payload, credentials: 'same-origin', ...init })
  if (res.status === 204) {
    return undefined as T
  }
  if (!res.ok) {
    // ログイン API 自体の 401 はフォームで扱う（toApiError は未ログインの通知をしない）
    throw await toApiError(res, path)
  }
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: <T = void>(path: string, body?: unknown) => request<T>('DELETE', path, body),
}

/** クエリ文字列を組み立てる。空の値は含めない */
export function query(params: Record<string, string | number | undefined | null>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}
