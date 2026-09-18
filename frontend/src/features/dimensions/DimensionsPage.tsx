import { Link } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'
import { link, mutedText, pageHeading } from '../../lib/ui'
import { AccountSection } from './AccountSection'
import { BusinessSection } from './BusinessSection'
import { DepartmentSection } from './DepartmentSection'

export function DimensionsPage() {
  const { user } = useAuth()
  const canEdit = user?.role === 'office_admin'

  return (
    <div className="mx-auto mt-10 max-w-3xl px-4">
      <p className="mb-4">
        <Link to="/" className={link}>
          ← ダッシュボード
        </Link>
      </p>
      <h1 className={pageHeading}>ディメンションマスタ管理</h1>
      {!canEdit && <p className={mutedText}>閲覧のみ（編集は事務局管理者のみ可能です）</p>}
      <BusinessSection canEdit={canEdit} />
      <DepartmentSection canEdit={canEdit} />
      <AccountSection canEdit={canEdit} />
    </div>
  )
}
