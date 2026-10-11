// Package versions は、作成中のシナリオ（年度ごとに1つ）を「今回の見込」「翌期の計画」「そのほか」に分ける
// （docs/plan.md「2.6」「2.25」）。シナリオ・通知・ホームで同じ判定を使う。
//
//   - 計画の版: 決算確定月がなく、エイリアスが「期初計画」の作成中の版
//   - 今回の見込: 計画の版でない作成中の版のうち、最も新しい年度の版。なければ、最も古い年度の作成中の版
//   - 翌期の計画: 今回の見込より後の年度の、計画の版（最も古い年度）
//   - そのほか: 上のどれでもない作成中の版（新年度の見込を始めた後の、締める前の前年度の版など）
package versions

import (
	"context"
	"database/sql"
)

// Version は作成中の版。
type Version struct {
	ID            int64
	FiscalYear    int
	PlanRole      *string
	ActualThrough *string
}

func (v Version) isPlan() bool {
	return v.ActualThrough == nil && v.PlanRole != nil && *v.PlanRole == "initial"
}

// Actives は作成中の版の分け方。
type Actives struct {
	Current  *Version
	NextPlan *Version
	Others   []Version
}

// Kind は版の種類（current / next_plan / other）。作成中でなければ空。
func (a Actives) Kind(id int64) string {
	switch {
	case a.Current != nil && a.Current.ID == id:
		return "current"
	case a.NextPlan != nil && a.NextPlan.ID == id:
		return "next_plan"
	}
	for _, o := range a.Others {
		if o.ID == id {
			return "other"
		}
	}
	return ""
}

// Querier は *sql.DB と *sql.Tx に共通する検索のメソッド。
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Load は作成中の版を読み、分ける。
func Load(ctx context.Context, q Querier) (Actives, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, fiscal_year, plan_role, DATE_FORMAT(actual_through, '%Y-%m') FROM scenarios WHERE is_active ORDER BY fiscal_year")
	if err != nil {
		return Actives{}, err
	}
	defer rows.Close()
	var list []Version
	for rows.Next() {
		var v Version
		var role, through sql.NullString
		if err := rows.Scan(&v.ID, &v.FiscalYear, &role, &through); err != nil {
			return Actives{}, err
		}
		if role.Valid {
			v.PlanRole = &role.String
		}
		if through.Valid {
			v.ActualThrough = &through.String
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return Actives{}, err
	}
	return Classify(list), nil
}

// Classify は作成中の版（年度の古い順）を分ける。
func Classify(list []Version) Actives {
	var a Actives
	for i := range list {
		if !list[i].isPlan() {
			a.Current = &list[i] // 年度の古い順なので、最後に残るのが最も新しい年度
		}
	}
	if a.Current == nil && len(list) > 0 {
		a.Current = &list[0]
	}
	for i := range list {
		v := &list[i]
		if a.Current != nil && v.ID == a.Current.ID {
			continue
		}
		if a.NextPlan == nil && v.isPlan() && a.Current != nil && v.FiscalYear > a.Current.FiscalYear {
			a.NextPlan = v
			continue
		}
		a.Others = append(a.Others, *v)
	}
	return a
}
