import { useAuth } from '../auth/useAuth'

export function DashboardPage() {
  const { user, logout } = useAuth()

  return (
    <div style={{ maxWidth: 480, margin: '80px auto' }}>
      <h1>FP&amp;A ダッシュボード</h1>
      <p>
        ログイン中: {user?.name}（{user?.role}）
      </p>
      <button type="button" onClick={() => void logout()}>
        ログアウト
      </button>
      <p style={{ marginTop: 32, color: '#666' }}>
        ディメンションマスタ管理・予実差異レポートは Phase 1 で実装予定です。
      </p>
    </div>
  )
}
