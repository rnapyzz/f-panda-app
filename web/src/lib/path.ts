// URL のパスとルート定義の照合（React に依存しない部分）。

/** pattern（例: "/activities/:id"）と pathname が一致すればパラメーターを返す */
export function matchPath(pattern: string, pathname: string): Record<string, string> | null {
  const p = pattern.split('/').filter(Boolean)
  const s = pathname.split('/').filter(Boolean)
  if (p.length !== s.length) return null
  const params: Record<string, string> = {}
  for (let i = 0; i < p.length; i++) {
    if (p[i].startsWith(':')) {
      params[p[i].slice(1)] = decodeURIComponent(s[i])
    } else if (p[i] !== s[i]) {
      return null
    }
  }
  return params
}
