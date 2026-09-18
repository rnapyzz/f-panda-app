import { Link } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'
import { AccountSection } from './AccountSection'
import { BusinessSection } from './BusinessSection'
import { DepartmentSection } from './DepartmentSection'

export function DimensionsPage() {
  const { user } = useAuth()
  const canEdit = user?.role === 'office_admin'

  return (
    <div style={{ maxWidth: 720, margin: '40px auto' }}>
      <p>
        <Link to="/">← ダッシュボード</Link>
      </p>
      <h1>ディメンションマスタ管理</h1>
      {!canEdit && <p style={{ color: '#666' }}>閲覧のみ（編集は事務局管理者のみ可能です）</p>}
      <BusinessSection canEdit={canEdit} />
      <DepartmentSection canEdit={canEdit} />
      <AccountSection canEdit={canEdit} />
    </div>
  )
}
