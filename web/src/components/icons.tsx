// メニュー用の線画アイコン（24×24、線の色は文字色に合わせる）。依存ライブラリは使わない。

import type { ReactNode } from 'react'

function Icon({ children, className = 'size-5' }: { children: ReactNode; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden="true">
      {children}
    </svg>
  )
}

type P = { className?: string }

/** 施策: チェックリスト */
export const IconActivities = (p: P) => (
  <Icon {...p}>
    <path d="M9 6h11M9 12h11M9 18h11" />
    <path d="M4 6l1 1 2-2M4 12l1 1 2-2M4 18l1 1 2-2" />
  </Icon>
)

/** シナリオ: 重なった層 */
export const IconScenarios = (p: P) => (
  <Icon {...p}>
    <path d="M12 3l9 5-9 5-9-5 9-5z" />
    <path d="M3 13l9 5 9-5" />
  </Icon>
)

/** 予実比較: 棒グラフ */
export const IconReports = (p: P) => (
  <Icon {...p}>
    <path d="M4 20h16" />
    <path d="M7 16v-5M12 16V6M17 16v-8" />
  </Icon>
)

/** リスク: 注意の三角 */
export const IconRisks = (p: P) => (
  <Icon {...p}>
    <path d="M12 4l9 16H3l9-16z" />
    <path d="M12 10v4M12 17h.01" />
  </Icon>
)

/** 変更履歴: 時計 */
export const IconHistory = (p: P) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 7.5V12l3 2" />
  </Icon>
)

/** 組織: ビル */
export const IconOrganizations = (p: P) => (
  <Icon {...p}>
    <path d="M5 21V4h10v17M15 9h4v12" />
    <path d="M3 21h18M8.5 8h3M8.5 12h3M8.5 16h3" />
  </Icon>
)

/** セグメント: 円グラフ */
export const IconSegments = (p: P) => (
  <Icon {...p}>
    <path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5H12V3.5z" />
    <path d="M15 3.8A8.5 8.5 0 0 1 20.2 9H15V3.8z" />
  </Icon>
)

/** ユニット: 箱 */
export const IconUnits = (p: P) => (
  <Icon {...p}>
    <path d="M4 8l8-4 8 4-8 4-8-4z" />
    <path d="M4 8v8l8 4 8-4V8M12 12v8" />
  </Icon>
)

/** 勘定科目: 台帳 */
export const IconSubjects = (p: P) => (
  <Icon {...p}>
    <path d="M6 3.5h11a1.5 1.5 0 0 1 1.5 1.5v14.5H6.5A1.5 1.5 0 0 1 5 18V4.5a1 1 0 0 1 1-1z" />
    <path d="M5 18a1.5 1.5 0 0 1 1.5-1.5h12M9 8h6M9 11.5h4" />
  </Icon>
)

/** ユーザー: 人物 */
export const IconUsers = (p: P) => (
  <Icon {...p}>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M3 20c.5-3.5 3-5.5 6-5.5s5.5 2 6 5.5" />
    <path d="M16 4.8a3.5 3.5 0 0 1 0 6.4M18.5 14.8c1.4.8 2.3 2.6 2.5 5.2" />
  </Icon>
)

/** ログアウト */
export const IconLogout = (p: P) => (
  <Icon {...p}>
    <path d="M14 4h4a1.5 1.5 0 0 1 1.5 1.5v13A1.5 1.5 0 0 1 18 20h-4" />
    <path d="M10 16l-4-4 4-4M6 12h10" />
  </Icon>
)

/** 折りたたむ（左向き）／広げる（右向き） */
export const IconCollapse = ({ collapsed, ...p }: P & { collapsed: boolean }) => (
  <Icon {...p}>
    <rect x="3.5" y="4.5" width="17" height="15" rx="2" />
    <path d="M9 4.5v15" />
    <path d={collapsed ? 'M13 10l2 2-2 2' : 'M15 10l-2 2 2 2'} />
  </Icon>
)

/** メニュー（ハンバーガー） */
export const IconMenu = (p: P) => (
  <Icon {...p}>
    <path d="M4 7h16M4 12h16M4 17h16" />
  </Icon>
)

/** 閉じる */
export const IconClose = (p: P) => (
  <Icon {...p}>
    <path d="M6 6l12 12M18 6L6 18" />
  </Icon>
)
