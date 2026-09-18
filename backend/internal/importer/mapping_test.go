package importer

import (
	"testing"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

func testMasters() (businesses []db.DimBusiness, departments []db.DimDepartment, accounts []db.DimAccount, periods []db.DimPeriod) {
	businesses = []db.DimBusiness{{ID: 1, Code: "B1", Name: "事業A"}}
	departments = []db.DimDepartment{{ID: 1, Code: "D1", Name: "部門X"}}
	accounts = []db.DimAccount{{ID: 1, Code: "A1", Name: "人件費"}, {ID: 2, Code: "A2", Name: "地代家賃"}}
	periods = []db.DimPeriod{
		{ID: 100, CalendarYear: 2026, CalendarMonth: 4},
		{ID: 101, CalendarYear: 2026, CalendarMonth: 5},
	}
	return
}

func TestResolveRowsSumsMatchingRowsAndReportsErrors(t *testing.T) {
	businesses, departments, accounts, periods := testMasters()
	table := ParsedTable{
		Headers: []string{"事業", "部門", "勘定科目", "期間", "金額"},
		Rows: [][]string{
			{"事業A", "部門X", "人件費", "2026-04", "600,000"},
			{"事業A", "部門X", "人件費", "2026-04", "400,000"}, // same key: should sum with the row above
			{"事業A", "部門X", "地代家賃", "2026/05", "500000"},
			{"事業A", "部門X", "存在しない科目", "2026-04", "1000"},     // unresolvable account
			{"事業A", "部門X", "人件費", "2099-13", "1000"},         // unresolvable period (invalid month)
			{"事業A", "部門X", "人件費", "2026-04", "not-a-number"}, // unresolvable amount
		},
	}
	mapping := ColumnMapping{BusinessColumn: 0, DepartmentColumn: 1, AccountColumn: 2, PeriodColumn: 3, AmountColumn: 4}

	result := resolveRows(table, mapping, businesses, departments, accounts, periods)

	if got := result.Amounts[dimKey{1, 1, 1, 100}]; got != 1_000_000 {
		t.Fatalf("人件費/2026-04 sum = %v, want 1000000", got)
	}
	if got := result.Amounts[dimKey{1, 1, 2, 101}]; got != 500_000 {
		t.Fatalf("地代家賃/2026-05 sum = %v, want 500000", got)
	}
	if len(result.Amounts) != 2 {
		t.Fatalf("expected exactly 2 resolved dimension combinations, got %d: %v", len(result.Amounts), result.Amounts)
	}
	if result.TotalErrors != 3 {
		t.Fatalf("expected 3 row errors, got %d: %+v", result.TotalErrors, result.Errors)
	}
}

func TestResolveRowsReportsShortRow(t *testing.T) {
	businesses, departments, accounts, periods := testMasters()
	table := ParsedTable{Rows: [][]string{{"事業A", "部門X"}}} // missing account/period/amount columns
	mapping := ColumnMapping{BusinessColumn: 0, DepartmentColumn: 1, AccountColumn: 2, PeriodColumn: 3, AmountColumn: 4}

	result := resolveRows(table, mapping, businesses, departments, accounts, periods)
	if result.TotalErrors != 1 {
		t.Fatalf("expected 1 error for a short row, got %d", result.TotalErrors)
	}
	if len(result.Amounts) != 0 {
		t.Fatalf("expected no resolved amounts, got %v", result.Amounts)
	}
}

func TestResolveRowsCapsReportedErrorsButCountsAll(t *testing.T) {
	businesses, departments, accounts, periods := testMasters()
	var rows [][]string
	for i := 0; i < maxReportedErrors+20; i++ {
		rows = append(rows, []string{"事業A", "部門X", "存在しない科目", "2026-04", "1000"})
	}
	table := ParsedTable{Rows: rows}
	mapping := ColumnMapping{BusinessColumn: 0, DepartmentColumn: 1, AccountColumn: 2, PeriodColumn: 3, AmountColumn: 4}

	result := resolveRows(table, mapping, businesses, departments, accounts, periods)
	if result.TotalErrors != maxReportedErrors+20 {
		t.Fatalf("TotalErrors = %d, want %d", result.TotalErrors, maxReportedErrors+20)
	}
	if len(result.Errors) != maxReportedErrors {
		t.Fatalf("len(Errors) = %d, want it capped at %d", len(result.Errors), maxReportedErrors)
	}
}

func TestResolvePeriodFormats(t *testing.T) {
	_, _, _, periods := testMasters()
	cases := []struct {
		input  string
		wantID uint64
		wantOk bool
	}{
		{"2026-04", 100, true},
		{"2026/04", 100, true},
		{"2026年4月", 100, true},
		{"202604", 100, true},
		{"2026-04-15", 100, true},
		{"2026-05", 101, true},
		{"not a period", 0, false},
		{"2026-13", 0, false}, // invalid month
		{"2099-04", 0, false}, // no matching dim_period fixture
	}
	for _, c := range cases {
		id, ok := resolvePeriod(c.input, periods)
		if ok != c.wantOk || (ok && id != c.wantID) {
			t.Errorf("resolvePeriod(%q) = (%v, %v), want (%v, %v)", c.input, id, ok, c.wantID, c.wantOk)
		}
	}
}

func TestParseAmountFormats(t *testing.T) {
	cases := []struct {
		input  string
		want   float64
		wantOk bool
	}{
		{"1000000", 1_000_000, true},
		{"1,000,000", 1_000_000, true},
		{"¥1,000,000", 1_000_000, true},
		{"1000000.50", 1_000_000.5, true},
		{"(1,000)", -1000, true}, // accounting negative notation
		{"-1000", -1000, true},
		{" 1,000 ", 1000, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		got, ok := parseAmount(c.input)
		if ok != c.wantOk || (ok && got != c.want) {
			t.Errorf("parseAmount(%q) = (%v, %v), want (%v, %v)", c.input, got, ok, c.want, c.wantOk)
		}
	}
}
