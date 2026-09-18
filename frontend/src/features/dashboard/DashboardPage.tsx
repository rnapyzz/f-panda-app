import { BarChart3, ClipboardCheck, Database, History, ListChecks, ScrollText, Table2, Upload, Users, type LucideIcon } from 'lucide-react'
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
  { to: '/admin/assignments', title: '担当割当て管理', description: '現場担当者ごとの担当事業×部門を設定します。', icon: Users },
  {
    to: '/admin/submission-status',
    title: '提出状況ダッシュボード',
    description: '事業×部門ごとの予算・見込・実績の提出状況を確認します。',
    icon: ClipboardCheck,
  },
  { to: '/admin/validation', title: 'バインディング検証結果', description: '全社の提出を横断して形状チェックの結果を確認します。', icon: ListChecks },
  { to: '/admin/audit-log', title: '監査ログ', description: 'ログイン・マスタ変更・提出などの操作履歴を確認します。', icon: ScrollText },
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
