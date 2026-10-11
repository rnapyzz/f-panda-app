package versions

import "testing"

func TestClassify(t *testing.T) {
	s := func(v string) *string { return &v }
	initial, latest := s("initial"), s("latest")
	tests := []struct {
		name                   string
		list                   []Version
		current, next, nOthers int64
	}{
		{"作成中なし", nil, 0, 0, 0},
		{"最初の年度の期初計画だけ", []Version{{ID: 1, FiscalYear: 2026, PlanRole: initial}}, 1, 0, 0},
		{"月次の見込だけ", []Version{{ID: 2, FiscalYear: 2026, PlanRole: latest, ActualThrough: s("2026-09")}}, 2, 0, 0},
		{"12月: 当期の見込と翌期の計画", []Version{{ID: 3, FiscalYear: 2026, PlanRole: latest, ActualThrough: s("2026-11")}, {ID: 4, FiscalYear: 2027, PlanRole: initial}}, 3, 4, 0},
		{"5月: 新年度の見込を始めた後（前年度の最後の見込はそのほか）", []Version{{ID: 5, FiscalYear: 2026, PlanRole: latest, ActualThrough: s("2027-03")}, {ID: 6, FiscalYear: 2027, PlanRole: latest, ActualThrough: s("2027-04")}}, 6, 0, 1},
	}
	for _, tt := range tests {
		a := Classify(tt.list)
		id := func(v *Version) int64 {
			if v == nil {
				return 0
			}
			return v.ID
		}
		if id(a.Current) != tt.current || id(a.NextPlan) != tt.next || int64(len(a.Others)) != tt.nOthers {
			t.Errorf("%s: current %d next %d others %d", tt.name, id(a.Current), id(a.NextPlan), len(a.Others))
		}
	}
}
