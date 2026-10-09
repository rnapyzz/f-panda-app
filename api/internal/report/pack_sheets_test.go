package report

import (
	"fmt"
	"testing"
)

func TestPeriodsOf(t *testing.T) {
	months := []string{"2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10", "2026-11", "2026-12", "2027-01", "2027-02", "2027-03"}
	tests := []struct {
		grain string
		want  string
	}{
		{"month", "4月:1 5月:1 6月:1 7月:1 8月:1 9月:1 10月:1 11月:1 12月:1 1月:1 2月:1 3月:1 通期:12"},
		{"quarter", "Q1:3 Q2:3 Q3:3 Q4:3 通期:12"},
		{"half", "上期:6 下期:6 通期:12"},
		{"year", "通期:12"},
	}
	for _, tt := range tests {
		got := ""
		for i, p := range periodsOf(months, tt.grain) {
			if i > 0 {
				got += " "
			}
			got += fmt.Sprintf("%s:%d", p.label, len(p.months))
		}
		if got != tt.want {
			t.Errorf("%s = %s, want %s", tt.grain, got, tt.want)
		}
	}
}
