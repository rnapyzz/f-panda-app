const CSRF_COOKIE_NAME = 'fpanda_csrf'

function readCookie(name: string): string | null {
  const match = document.cookie
    .split('; ')
    .find((row) => row.startsWith(`${name}=`))
  return match ? decodeURIComponent(match.split('=')[1]) : null
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (init.body) {
    headers.set('Content-Type', 'application/json')
  }

  const method = (init.method ?? 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') {
    const csrfToken = readCookie(CSRF_COOKIE_NAME)
    if (csrfToken) {
      headers.set('X-CSRF-Token', csrfToken)
    }
  }

  const res = await fetch(`/api${path}`, {
    ...init,
    headers,
    credentials: 'include',
  })

  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(res.status, text || res.statusText)
  }

  if (res.status === 204) {
    return undefined as T
  }

  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }),
}
