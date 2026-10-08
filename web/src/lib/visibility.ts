// 閲覧の制限（docs/plan.md「2.17」）。

import type { Role, Subject } from '../api/types.ts'

/** 閲覧制限のある科目の金額と、すべての変更履歴を見られるロール（FP&A と経営陣） */
export const seesAll = (role: Role): boolean => role === 'fpa_admin' || role === 'viewer'

/** 施策の変更履歴を開けるか（FP&A・経営陣、または施策を編集できる人） */
export const canOpenHistory = (role: Role, canEditActivity: boolean): boolean => seesAll(role) || canEditActivity

/** 金額を入力・内訳を作成できる科目（閲覧制限のある科目は FP&A のみ） */
export const inputSubjects = (subjects: Subject[], role: Role): Subject[] => (role === 'fpa_admin' ? subjects : subjects.filter((s) => !s.is_restricted))
