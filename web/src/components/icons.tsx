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

/** シナリオ管理: スライダー */
export const IconAdmin = (p: P) => (
  <Icon {...p}>
    <path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0" />
    <circle cx="16" cy="6" r="2" />
    <circle cx="10" cy="12" r="2" />
    <circle cx="18" cy="18" r="2" />
  </Icon>
)

/** 確度の段階: メーター */
export const IconConfidence = (p: P) => (
  <Icon {...p}>
    <path d="M4 17a8 8 0 1 1 16 0" />
    <path d="M12 17l4-5" />
    <path d="M4 20h16" />
  </Icon>
)

/** ホーム: 家 */
export const IconHome = (p: P) => (
  <Icon {...p}>
    <path d="M4 11l8-7 8 7" />
    <path d="M6 9.5V20h12V9.5M10 20v-5h4v5" />
  </Icon>
)

/** 会計科目: 台帳 */
export const IconLedger = (p: P) => (
  <Icon {...p}>
    <rect x="4.5" y="3.5" width="15" height="17" rx="1.5" />
    <path d="M8 3.5v17M11 8h5.5M11 12h5.5M11 16h3.5" />
  </Icon>
)

/** 割当ルール: 分岐する矢印 */
export const IconRules = (p: P) => (
  <Icon {...p}>
    <path d="M4 12h6M10 12l5-5h4.5M10 12l5 5h4.5" />
    <path d="M17.5 5l2 2-2 2M17.5 15l2 2-2 2" />
  </Icon>
)

/** 実績の割当: 受け皿に入る */
export const IconAllocate = (p: P) => (
  <Icon {...p}>
    <path d="M12 3.5v9M8.5 9l3.5 3.5L15.5 9" />
    <path d="M4 14.5v3A2 2 0 0 0 6 19.5h12a2 2 0 0 0 2-2v-3" />
  </Icon>
)

/** 通知の設定: ベル */
export const IconBell = (p: P) => (
  <Icon {...p}>
    <path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 2h-15z" />
    <path d="M10 20.5a2 2 0 0 0 4 0" />
  </Icon>
)

/** 組織変更の予約: 予定表と矢印 */
export const IconOrgChange = (p: P) => (
  <Icon {...p}>
    <rect x="3.5" y="5" width="13" height="13" rx="1.5" />
    <path d="M3.5 9h13M7 3.5v3M13 3.5v3" />
    <path d="M14.5 15h6M18 12.5l2.5 2.5-2.5 2.5" />
  </Icon>
)

/** 使い方: 開いた本 */
export const IconManual = (p: P) => (
  <Icon {...p}>
    <path d="M12 6c-2-1.5-5-2-8-2v14c3 0 6 .5 8 2 2-1.5 5-2 8-2V4c-3 0-6 .5-8 2z" />
    <path d="M12 6v14" />
  </Icon>
)
