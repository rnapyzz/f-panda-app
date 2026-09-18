import { BarChart3, Database, History, Table2, Upload, type LucideIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { card, mutedText, pageHeading } from '../../lib/ui'
import { useAuth } from '../auth/useAuth'

interface QuickLink {
  to: string
  title: string
  description: string
  icon: LucideIcon
}

const businessLinks: QuickLink[] = [
  { to: '/sheets', title: 'マイシート', description: '自由レイアウトのスプレッドシートでデータを入力・提出します。', icon: Table2 },
  { to: '/variance', title: '予実差異レポート', description: '予算・見込・実績を比較し、差異を確認します。', icon: BarChart3 },
]

const adminLinks: QuickLink[] = [
  { to: '/dimensions', title: 'ディメンションマスタ管理', description: '事業・部門・勘定科目・サービス・施策のマスタデータを管理します。', icon: Database },
  { to: '/versions', title: 'バージョン管理', description: '予算・見込・実績のバージョンを作成し、提出・確定を行います。', icon: History },
  { to: '/import', title: '実績インポート', description: 'CSV/XLSXファイルから実績データを取り込みます。', icon: Upload },
]

export function DashboardPage() {
  const { user } = useAuth()

  const links: QuickLink[] =
    user?.role === 'office_admin'
      ? [...businessLinks, ...adminLinks]
      : [
          ...businessLinks,
          {
            to: '/dimensions',
            title: 'ディメンションマスタ管理',
            description: '事業・部門・勘定科目・サービス・施策のマスタデータを閲覧します。',
            icon: Database,
          },
        ]

  return (
    <div>
      <h1 className={pageHeading}>ようこそ、{user?.name} さん</h1>
      <p className={mutedText}>左のメニューから各機能にアクセスできます。</p>

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {links.map((item) => (
          <Link key={item.to} to={item.to} className={`${card} transition-shadow hover:shadow-md`}>
            <item.icon size={22} className="text-accent" />
            <p className="mt-3 font-semibold text-gray-900 dark:text-gray-100">{item.title}</p>
            <p className={`${mutedText} mt-1`}>{item.description}</p>
          </Link>
        ))}
      </div>
    </div>
  )
}
