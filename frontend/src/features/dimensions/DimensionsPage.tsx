import { useAuth } from '../auth/useAuth'
import { mutedText, pageHeading } from '../../lib/ui'
import { AccountSection } from './AccountSection'
import { BusinessSection } from './BusinessSection'
import { DepartmentSection } from './DepartmentSection'

export function DimensionsPage() {
  const { user } = useAuth()
  const canEdit = user?.role === 'office_admin'

  return (
    <div>
      <h1 className={pageHeading}>ディメンションマスタ管理</h1>
      {!canEdit && <p className={mutedText}>閲覧のみ（編集は事務局管理者のみ可能です）</p>}
      <BusinessSection canEdit={canEdit} />
      <DepartmentSection canEdit={canEdit} />
      <AccountSection canEdit={canEdit} />
    </div>
  )
}
