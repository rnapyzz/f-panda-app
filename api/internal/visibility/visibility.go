// Package visibility は閲覧の制限（docs/plan.md「2.17」）の共通処理を提供する。
//
// 閲覧制限のある科目（subjects.is_restricted）の金額は、FP&A と経営陣・レビュアーだけが見られる。
// それ以外のロールには、金額を返す API のどこでも、その科目を除いて（合計からも除いて）返す。
package visibility

import (
	"context"

	"github.com/rnapyzz/f-panda-app/api/internal/auth"
	"github.com/rnapyzz/f-panda-app/api/internal/dbx"
)

// HidesRestricted は、ユーザーに閲覧制限のある科目の金額を見せないかを返す。
func HidesRestricted(u auth.User) bool {
	return u.Role != auth.RoleFPAAdmin && u.Role != auth.RoleViewer
}

// SubjectFilter は、ユーザーが見られない科目を除く SQL の条件（" AND ..."）を返す。
// col は科目 ID の列（例: "b.subject_id"）。すべて見られるユーザーには空文字を返す。
func SubjectFilter(u auth.User, col string) string {
	if !HidesRestricted(u) {
		return ""
	}
	return " AND " + col + " NOT IN (SELECT rs.id FROM subjects rs WHERE rs.is_restricted)"
}

// Hidden は、ユーザーの画面で閲覧制限のある科目を除いているか（レスポンスの restricted_hidden）を返す。
// 見られないユーザーで、閲覧制限のある科目が1つでもあれば true。
func Hidden(ctx context.Context, q dbx.Querier, u auth.User) (bool, error) {
	if !HidesRestricted(u) {
		return false, nil
	}
	var n int
	err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM subjects WHERE is_restricted").Scan(&n)
	return n > 0, err
}

// IsRestricted は、科目に閲覧制限があるかを返す。
func IsRestricted(ctx context.Context, q dbx.Querier, subjectID int64) (bool, error) {
	var restricted bool
	err := q.QueryRowContext(ctx, "SELECT is_restricted FROM subjects WHERE id = ?", subjectID).Scan(&restricted)
	return restricted, err
}
