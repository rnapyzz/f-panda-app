package calc

import (
	"reflect"
	"testing"
)

func TestFiscalMonths(t *testing.T) {
	got := FiscalMonths(2026)
	want := []string{"2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10", "2026-11", "2026-12", "2027-01", "2027-02", "2027-03"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FiscalMonths(2026) = %v", got)
	}
}
