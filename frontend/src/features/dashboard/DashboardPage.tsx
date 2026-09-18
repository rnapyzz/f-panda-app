import { Link } from 'react-router-dom'
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
      <ul style={{ marginTop: 32 }}>
        <li>
          <Link to="/dimensions">ディメンションマスタ管理</Link>
        </li>
        <li>
          <Link to="/entry">データ入力（簡易フォーム）</Link>
        </li>
        <li>
          <Link to="/variance">予実差異レポート</Link>
        </li>
      </ul>
    </div>
  )
}
