import { Link } from 'react-router-dom'
import { buttonSecondary, link, mutedText, pageHeading } from '../../lib/ui'
import { useAuth } from '../auth/useAuth'

export function DashboardPage() {
  const { user, logout } = useAuth()

  return (
    <div className="mx-auto mt-16 max-w-md px-4">
      <h1 className={pageHeading}>FP&amp;A ダッシュボード</h1>
      <p className={mutedText}>
        ログイン中: {user?.name}（{user?.role}）
      </p>
      <button type="button" className={`${buttonSecondary} mt-3`} onClick={() => void logout()}>
        ログアウト
      </button>
      <ul className="mt-8 space-y-2">
        <li>
          <Link to="/dimensions" className={link}>
            ディメンションマスタ管理
          </Link>
        </li>
        <li>
          <Link to="/entry" className={link}>
            データ入力（簡易フォーム）
          </Link>
        </li>
        <li>
          <Link to="/variance" className={link}>
            予実差異レポート
          </Link>
        </li>
      </ul>
    </div>
  )
}
