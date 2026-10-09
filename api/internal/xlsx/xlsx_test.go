package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"math/big"
	"strings"
	"testing"
)

func TestCellRef(t *testing.T) {
	for _, tc := range []struct {
		row, col int
		want     string
	}{{0, 0, "A1"}, {9, 25, "Z10"}, {0, 26, "AA1"}, {1, 27, "AB2"}, {0, 701, "ZZ1"}, {0, 702, "AAA1"}} {
		if got := CellRef(tc.row, tc.col); got != tc.want {
			t.Errorf("CellRef(%d, %d) = %s, want %s", tc.row, tc.col, got, tc.want)
		}
	}
}

func TestWrite(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []Sheet{
		{
			Name:       "全社 P/L",
			Rows:       [][]Cell{{T("科目", Header), T("A社 & <B>", Header)}, {Indent("売上", 1), N(big.NewInt(-1234567), Number)}, {T("合計", LabelSum), N(big.NewInt(0), NumberSum)}},
			Widths:     []float64{20, 14},
			FreezeRows: 1, FreezeCols: 1,
			Merges: []Merge{{Row: 0, Col: 0, Rows: 1, Cols: 2}},
		},
		{Name: "条件/テスト:長い名前のシート名は31文字までに切り詰める必要がある", Rows: [][]Cell{{T("年度", Bold), T("2026\x01年度", Plain)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
		// どの部品も XML として読めること
		d := xml.NewDecoder(bytes.NewReader(b))
		for {
			if _, err := d.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: XML として読めません: %v", f.Name, err)
			}
		}
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s がありません", name)
		}
	}
	s1 := files["xl/worksheets/sheet1.xml"]
	for _, want := range []string{`<c r="B2" s="3"><v>-1234567</v></c>`, `A社 &amp; &lt;B&gt;`, `<mergeCell ref="A1:B1"/>`, `xSplit="1" ySplit="1" topLeftCell="B2"`, `<col min="1" max="1" width="20.0" customWidth="1"/>`} {
		if !strings.Contains(s1, want) {
			t.Errorf("sheet1 に %q がない", want)
		}
	}
	if !strings.Contains(files["xl/workbook.xml"], `name="条件_テスト_長い名前のシート名は31文字までに切り詰める必要`) {
		t.Errorf("シート名 = %s", files["xl/workbook.xml"])
	}
	if strings.Contains(files["xl/worksheets/sheet2.xml"], "\x01") {
		t.Error("制御文字が残っている")
	}
}
