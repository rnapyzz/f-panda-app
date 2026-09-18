package inputsheet

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// workbookSnapshot mirrors just the parts of Univer's IWorkbookData JSON
// shape this package needs to read cell values back out of a saved sheet.
// Cell coordinates are string-keyed in the JSON (Univer's
// IObjectMatrixPrimitiveType<T> is `{ [row: number]: { [col: number]: T } }`,
// which JSON serializes with numeric keys as strings).
type workbookSnapshot struct {
	Sheets map[string]sheetSnapshot `json:"sheets"`
}

type sheetSnapshot struct {
	Name     string                         `json:"name"`
	CellData map[string]map[string]cellData `json:"cellData"`
}

type cellData struct {
	V any `json:"v"`
}

// findSheetByName returns the first sheet in the workbook whose display
// name matches, since Univer keys the sheets map by internal sheet id, not
// name, and a binding stores the human-readable sheet name.
func (w workbookSnapshot) findSheetByName(name string) (sheetSnapshot, bool) {
	for _, sheet := range w.Sheets {
		if sheet.Name == name {
			return sheet, true
		}
	}
	return sheetSnapshot{}, false
}

func (s sheetSnapshot) cellText(row, col int32) string {
	return cellToText(s.cellValue(row, col))
}

func (s sheetSnapshot) cellValue(row, col int32) any {
	cols, ok := s.CellData[strconv.Itoa(int(row))]
	if !ok {
		return nil
	}
	cell, ok := cols[strconv.Itoa(int(col))]
	if !ok {
		return nil
	}
	return cell.V
}

func cellToText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return fmt.Sprint(x)
	default:
		return fmt.Sprint(x)
	}
}

// cellToFloat parses a cell's raw value as a number, treating an empty or
// missing cell as "no value" (ok=false) rather than zero, so a blank data
// cell in the bound range is simply skipped instead of writing a bogus 0.
func cellToFloat(v any) (amount float64, ok bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		trimmed := strings.TrimSpace(x)
		if trimmed == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func parseWorkbookSnapshot(raw string) (workbookSnapshot, error) {
	var w workbookSnapshot
	if err := json.Unmarshal([]byte(raw), &w); err != nil {
		return workbookSnapshot{}, fmt.Errorf("parse sheet snapshot: %w", err)
	}
	return w, nil
}
