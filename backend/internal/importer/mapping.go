package importer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

// ColumnMapping identifies which column (0-based index into a parsed row)
// holds each dimension, as chosen by the user in the column-mapping wizard.
// JSON tags matter here: this is unmarshaled directly from the frontend's
// multipart "mapping" field, and without them the (snake_case) JSON keys
// wouldn't match these (PascalCase) field names, silently leaving every
// column index at its zero value instead of erroring.
type ColumnMapping struct {
	BusinessColumn   int `json:"business_column"`
	DepartmentColumn int `json:"department_column"`
	AccountColumn    int `json:"account_column"`
	PeriodColumn     int `json:"period_column"`
	AmountColumn     int `json:"amount_column"`
}

func (m ColumnMapping) maxColumn() int {
	max := m.BusinessColumn
	for _, c := range []int{m.DepartmentColumn, m.AccountColumn, m.PeriodColumn, m.AmountColumn} {
		if c > max {
			max = c
		}
	}
	return max
}

type RowError struct {
	RowNumber int    `json:"row_number"` // 1-based, counting from the first data row (excludes the header row)
	Message   string `json:"message"`
}

const maxReportedErrors = 100

type dimKey struct {
	businessID, departmentID, accountID, periodID uint64
}

// ResolveResult is the outcome of resolving every row in a parsed table
// against the column mapping and the current dimension masters.
type ResolveResult struct {
	Amounts     map[dimKey]float64 // summed per unique dimension combination
	Errors      []RowError
	TotalErrors int // may exceed len(Errors) if the list was capped
}

// resolveRows walks every data row, resolves business/department/account by
// exact code-or-name match and period by parsing the cell text as a
// year+month, and sums amounts per unique (business, department, account,
// period) combination — a row that fails to resolve is recorded as an
// error and excluded, but doesn't stop the rest of the file from importing.
func resolveRows(table ParsedTable, mapping ColumnMapping, businesses []db.DimBusiness, departments []db.DimDepartment, accounts []db.DimAccount, periods []db.DimPeriod) ResolveResult {
	businessByKey := indexBusinesses(businesses)
	departmentByKey := indexDepartments(departments)
	accountByKey := indexAccounts(accounts)

	result := ResolveResult{Amounts: map[dimKey]float64{}}

	for i, row := range table.Rows {
		rowNum := i + 1
		if mapping.maxColumn() >= len(row) {
			result.addError(rowNum, "列数が不足しています")
			continue
		}

		businessID, ok := businessByKey[normalizeKey(row[mapping.BusinessColumn])]
		if !ok {
			result.addError(rowNum, fmt.Sprintf("事業 %q に一致するマスタが見つかりません", row[mapping.BusinessColumn]))
			continue
		}
		departmentID, ok := departmentByKey[normalizeKey(row[mapping.DepartmentColumn])]
		if !ok {
			result.addError(rowNum, fmt.Sprintf("部門 %q に一致するマスタが見つかりません", row[mapping.DepartmentColumn]))
			continue
		}
		accountID, ok := accountByKey[normalizeKey(row[mapping.AccountColumn])]
		if !ok {
			result.addError(rowNum, fmt.Sprintf("勘定科目 %q に一致するマスタが見つかりません", row[mapping.AccountColumn]))
			continue
		}
		periodID, ok := resolvePeriod(row[mapping.PeriodColumn], periods)
		if !ok {
			result.addError(rowNum, fmt.Sprintf("期間 %q を解釈できません", row[mapping.PeriodColumn]))
			continue
		}
		amount, ok := parseAmount(row[mapping.AmountColumn])
		if !ok {
			result.addError(rowNum, fmt.Sprintf("金額 %q を数値として解釈できません", row[mapping.AmountColumn]))
			continue
		}

		key := dimKey{businessID, departmentID, accountID, periodID}
		result.Amounts[key] += amount
	}

	return result
}

func (r *ResolveResult) addError(rowNum int, message string) {
	r.TotalErrors++
	if len(r.Errors) < maxReportedErrors {
		r.Errors = append(r.Errors, RowError{RowNumber: rowNum, Message: message})
	}
}

func normalizeKey(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func indexBusinesses(items []db.DimBusiness) map[string]uint64 {
	m := make(map[string]uint64, len(items)*2)
	for _, it := range items {
		m[normalizeKey(it.Code)] = it.ID
		m[normalizeKey(it.Name)] = it.ID
	}
	return m
}

func indexDepartments(items []db.DimDepartment) map[string]uint64 {
	m := make(map[string]uint64, len(items)*2)
	for _, it := range items {
		m[normalizeKey(it.Code)] = it.ID
		m[normalizeKey(it.Name)] = it.ID
	}
	return m
}

func indexAccounts(items []db.DimAccount) map[string]uint64 {
	m := make(map[string]uint64, len(items)*2)
	for _, it := range items {
		m[normalizeKey(it.Code)] = it.ID
		m[normalizeKey(it.Name)] = it.ID
	}
	return m
}

// periodDigitsRe extracts a 4-digit year followed (after 0-2 separator
// characters, e.g. "-", "/", "年") by a 1-2 digit month, so it matches
// "2026-04", "2026/04", "2026年4月", "202604", and "2026-04-01" alike (the
// trailing day, if any, is simply ignored). The greedy \d{4} and \d{1,2}
// bounds do the right thing on runs of digits with no separator at all
// (e.g. "202604" or "20260401") without needing a trailing boundary.
var periodDigitsRe = regexp.MustCompile(`(\d{4})\D{0,2}(\d{1,2})`)

func resolvePeriod(s string, periods []db.DimPeriod) (uint64, bool) {
	m := periodDigitsRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	year, _ := strconv.Atoi(m[1])
	month, err := strconv.Atoi(m[2])
	if err != nil || month < 1 || month > 12 {
		return 0, false
	}
	for _, p := range periods {
		if int(p.CalendarYear) == year && int(p.CalendarMonth) == month {
			return p.ID, true
		}
	}
	return 0, false
}

func parseAmount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	negative := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative = true
		s = s[1 : len(s)-1]
	}
	s = strings.NewReplacer(",", "", "¥", "", "￥", "", " ", "", "　", "").Replace(s)
	if s == "" {
		return 0, false
	}
	amount, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		amount = -amount
	}
	return amount, true
}
