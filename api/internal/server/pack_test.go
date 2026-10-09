package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// xlsxSheets は報告資料の Excel を読み、シート名 → 行 → 列の文字列（数値も文字列）を返す。
func xlsxSheets(t *testing.T, b []byte) map[string][][]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	read := func(name string) []byte {
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				defer rc.Close()
				b, _ := io.ReadAll(rc)
				return b
			}
		}
		t.Fatalf("%s がない", name)
		return nil
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(read("xl/workbook.xml"), &wb); err != nil {
		t.Fatal(err)
	}
	out := map[string][][]string{}
	for i, s := range wb.Sheets {
		var ws struct {
			Rows []struct {
				Cells []struct {
					Ref    string `xml:"r,attr"`
					Value  string `xml:"v"`
					Inline string `xml:"is>t"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err := xml.Unmarshal(read(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)), &ws); err != nil {
			t.Fatal(err)
		}
		var rows [][]string
		for _, r := range ws.Rows {
			var row []string
			for _, c := range r.Cells {
				col := 0
				for _, ch := range strings.TrimRight(c.Ref, "0123456789") {
					col = col*26 + int(ch-'A'+1)
				}
				for len(row) < col {
					row = append(row, "")
				}
				row[col-1] = c.Value + c.Inline
			}
			rows = append(rows, row)
		}
		out[s.Name] = rows
	}
	return out
}

// findRow は先頭の列が label の行を返す（字下げは無視）。
func findRow(rows [][]string, label string) []string {
	for _, r := range rows {
		if len(r) > 0 && r[0] == label {
			return r
		}
	}
	return nil
}

func (c *client) getXLSX(path string) (int, http.Header, []byte) {
	c.t.Helper()
	res, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

// TestReportPack は報告資料の Excel（docs/plan.md「2.23」）を確かめる。
func TestReportPack(t *testing.T) {
	f := newScenarioFixture(t)
	a := f.admin
	amounts := func(sid, aid int64, items ...map[string]any) {
		t.Helper()
		if status, body := a.do("PUT", f.valuesPath(sid, aid)+"/amounts", map[string]any{"reason": "r", "amounts": items}); status != http.StatusOK {
			t.Fatalf("amounts: status = %d, body = %v", status, body)
		}
	}
	amt := func(subject int64, month string, amount int) map[string]any {
		return map[string]any{"subject_id": subject, "target_month": month, "amount": amount}
	}
	personnel := a.mustCreate("/api/subjects", map[string]any{"code": "8210", "name": "人件費", "category": "expense", "is_restricted": true})
	other := a.mustCreate("/api/activities", activityBody(f.fn2, "OTHER-1", map[string]any{"owner_user_id": f.memberID}))

	// 目標（最初のシナリオ）と、今回の見込（作成中にする）
	amounts(f.budget, f.manualAct, amt(f.sales, "2026-04", 1000), amt(f.sales, "2026-10", 2000), amt(f.cost, "2026-04", 300), amt(personnel, "2026-05", 100))
	amounts(f.budget, f.formulaAct, amt(f.cost, "2026-04", 50))
	amounts(f.budget, other, amt(f.sales, "2026-07", 700))
	forecast := a.mustCreate("/api/scenarios", map[string]any{"name": "10月見込", "fiscal_year": 2026, "base_scenario_id": f.budget})
	amounts(forecast, f.manualAct, amt(f.sales, "2026-10", 2500))
	if status, body := a.do("POST", fmt.Sprintf("/api/scenarios/%d/activate", forecast), nil); status != http.StatusOK {
		t.Fatalf("activate: %d %v", status, body)
	}
	if status, body := a.do("PUT", f.valuesPath(forecast, f.manualAct)+"/note", map[string]any{"explanation": "10月の受注が増えた", "causes": []string{"volume"}}); status != http.StatusOK {
		t.Fatalf("note: %d %v", status, body)
	}

	path := fmt.Sprintf("/api/reports/pack.xlsx?scenario_ids=%d,%d&grain=year&activities=true", f.budget, forecast)
	status, header, b := a.getXLSX(path)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, b)
	}
	if cd := header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "filename*=UTF-8''%E5%A0%B1%E5%91%8A%E8%B3%87%E6%96%99_2026_") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	sheets := xlsxSheets(t, b)
	for _, name := range []string{"条件", "全社 P／L", "ユニット別 P／L", "変動の説明"} {
		if sheets[name] == nil {
			t.Fatalf("シート %s がない: %v", name, sheets)
		}
	}

	// 全社 P/L（通期）: 列は 目標・今回・差。予実比較と同じ集計
	pl := sheets["全社 P／L"]
	tests := []struct {
		label string
		want  []string
	}{
		{"受託売上", []string{"受託売上", "3700", "4200", "500"}},
		{"人件費", []string{"人件費", "100", "100", "0"}},
		{"収益計", []string{"収益計", "3700", "4200", "500"}},
		{"費用計", []string{"費用計", "450", "450", "0"}},
		{"利益", []string{"利益", "3250", "3750", "500"}},
	}
	for _, tt := range tests {
		if got := findRow(pl, tt.label); strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("全社 P/L %s = %v, want %v", tt.label, got, tt.want)
		}
	}
	// ユニット別 P/L: セグメント → ユニット → 施策。列は系列ごとの売上・利益と利益の差
	units := sheets["ユニット別 P／L"]
	for label, want := range map[string]string{
		"XXX事業": "XXX事業,3700,3250,4200,3750,500",
		"CCC課":  "CCC課,3000,2550,3500,3050,500",
		"DDD課":  "DDD課,700,700,700,700,0",
		"合計":    "合計,3700,3250,4200,3750,500",
	} {
		if got := findRow(units, label); strings.Join(got, ",") != want {
			t.Errorf("ユニット別 P/L %s = %v, want %s", label, got, want)
		}
	}
	activityRow := false
	for _, r := range units {
		if len(r) > 0 && strings.HasPrefix(r[0], "PRJ-1 ") {
			activityRow = strings.Join(r[1:], ",") == "3000,2600,3500,3100,500"
		}
	}
	if !activityRow {
		t.Errorf("施策の行がない・違う: %v", units)
	}
	// 変動の説明: 今回の見込の施策ごと。差の大きい順
	notes := sheets["変動の説明"]
	if len(notes) < 4 || notes[3][0] != "PRJ-1" || notes[3][4] != "入力中" || notes[3][6] != "500" || notes[3][8] != "数量・単価の増減" || notes[3][9] != "10月の受注が増えた" {
		t.Errorf("変動の説明 = %v", notes)
	}

	// 範囲（ユニット）と、閲覧制限: 現場には人件費を出さず、条件のシートに注記する
	path = fmt.Sprintf("/api/reports/pack.xlsx?scenario_ids=%d&grain=quarter&unit_id=%d&sheets=pl", f.budget, f.fn1)
	status, _, b = f.member.getXLSX(path)
	if status != http.StatusOK {
		t.Fatalf("member: status = %d, body = %s", status, b)
	}
	sheets = xlsxSheets(t, b)
	if sheets["ユニット別 P／L"] != nil || sheets["変動の説明"] != nil {
		t.Errorf("選んでいないシートがある")
	}
	pl = sheets["全社 P／L"]
	if findRow(pl, "人件費") != nil {
		t.Errorf("現場に閲覧制限のある科目が出ている")
	}
	// 四半期: Q1〜Q4 と通期（系列が1つなので差の列はない）
	if got := strings.Join(findRow(pl, "収益計"), ","); got != "収益計,1000,0,2000,0,3000" {
		t.Errorf("ユニットの収益計 = %s", got)
	}
	if got := strings.Join(findRow(pl, "費用計"), ","); got != "費用計,350,0,0,0,350" {
		t.Errorf("ユニットの費用計 = %s", got)
	}
	cond := sheets["条件"]
	if findRow(cond, "閲覧制限のある科目を除いた金額です") == nil || !strings.Contains(strings.Join(findRow(cond, "範囲"), ","), "CCC課") {
		t.Errorf("条件 = %v", cond)
	}

	// 条件の誤り
	for _, q := range []string{"grain=week", "sheets=pl,foo", fmt.Sprintf("unit_id=%d&segment_id=1", f.fn1)} {
		status, _, b := a.getXLSX(fmt.Sprintf("/api/reports/pack.xlsx?scenario_ids=%d&%s", f.budget, q))
		if status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, body = %s", q, status, b)
		}
	}
	if status, _, b := a.getXLSX("/api/reports/pack.xlsx?scenario_ids=" + fmt.Sprint(f.budget) + "&unit_id=99999"); status != http.StatusUnprocessableEntity {
		t.Errorf("存在しないユニット: status = %d, body = %s", status, b)
	}
}
