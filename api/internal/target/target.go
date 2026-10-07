// Package target は、シナリオで更新が必要な施策（更新の対象、docs/plan.md「2.10」）の条件を提供する。
//
// 施策のステータスが「完了」「中止」でない施策。ただし「完了」「中止」でも、そのシナリオの計画値の月（決算確定月より後）に
// 0 でない金額が残っている施策は対象にする。ホームの一覧・件数、締切の通知、シナリオの切り替えの注意で同じ条件を使う。
package target

// Condition は、施策（別名 alias）が更新の対象であることを表す SQL の条件。プレースホルダーの引数はシナリオ ID が1つ。
func Condition(alias string) string {
	return `(` + alias + `.status NOT IN ('completed', 'cancelled') OR EXISTS (
		SELECT 1 FROM budget_facts tbf JOIN scenarios tsc ON tsc.id = tbf.scenario_id
		WHERE tbf.scenario_id = ? AND tbf.activity_id = ` + alias + `.id AND tbf.amount <> 0
		  AND (tsc.actual_through IS NULL OR tbf.target_month > tsc.actual_through)))`
}
