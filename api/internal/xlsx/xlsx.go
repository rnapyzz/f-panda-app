// Package xlsx は、報告資料（docs/plan.md「2.23」）の Excel ファイル（Office Open XML、.xlsx）を書く。
//
// 外部ライブラリは使わず、archive/zip と XML の文字列で、必要な最小限（ワークブック・シート・スタイル）だけを書く。
// 文字列はセルの中に直接書き（inlineStr）、共有文字列の表は持たない。スタイルは決まった種類（Style）から選ぶ。
package xlsx

import (
	"archive/zip"
	"fmt"
	"io"
	"math/big"
	"strings"
)

// Style はセルの書式。styles.xml の cellXfs の並びと同じ順番。
type Style int

const (
	Plain       Style = iota // 標準
	Bold                     // 太字
	Header                   // 見出し: 太字・薄い灰色の塗り・下罫線・中央
	Number                   // 金額: 3桁区切り、マイナスは赤の ▲
	NumberSum                // 合計の金額: 太字・上罫線
	LabelSum                 // 合計の見出し: 太字・上罫線
	Indent1                  // 字下げ 1
	Indent2                  // 字下げ 2
	Indent3                  // 字下げ 3
	Wrap                     // 折り返し・上揃え（長い説明）
	Title                    // シートの題: 太字・大きめ
	Muted                    // 補足: 灰色の文字
	NumberBold               // 小計の金額: 太字
	BoldIndent1              // 小計の見出し: 太字・字下げ 1
	BoldIndent2              // 小計の見出し: 太字・字下げ 2
)

// Cell は1つのセル。Num が nil でなければ数値、そうでなければ Text の文字列。
type Cell struct {
	Text  string
	Num   *big.Int
	Style Style
}

// T は文字列のセル、N は数値のセル、Empty は値のないセル（罫線などの書式だけ）を作る。
func T(text string, style Style) Cell { return Cell{Text: text, Style: style} }
func N(v *big.Int, style Style) Cell  { return Cell{Num: v, Style: style} }
func Empty(style Style) Cell          { return Cell{Style: style} }

// Indent は字下げした文字列のセル（level は 0〜3。0 は字下げなし）。
func Indent(text string, level int) Cell {
	if level <= 0 {
		return T(text, Plain)
	}
	return T(text, Indent1+Style(min(level, 3)-1))
}

// BoldIndent は字下げした太字の見出し（小計の行）。level は 0〜2（それより深いときは 2）。
func BoldIndent(text string, level int) Cell {
	if level <= 0 {
		return T(text, Bold)
	}
	return T(text, BoldIndent1+Style(min(level, 2)-1))
}

// Merge は結合するセルの範囲（0 始まりの行・列）。
type Merge struct{ Row, Col, Rows, Cols int }

// Sheet は1枚のシート。
type Sheet struct {
	Name string
	Rows [][]Cell
	// Widths は列の幅（文字数）。0 は既定
	Widths []float64
	// FreezeRows・FreezeCols は、上・左から固定する行・列の数
	FreezeRows, FreezeCols int
	Merges                 []Merge
}

// Write はシートを .xlsx として書き出す。
func Write(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		return fmt.Errorf("xlsx: シートがありません")
	}
	zw := zip.NewWriter(w)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes(len(sheets))},
		{"_rels/.rels", rootRels},
		{"xl/workbook.xml", workbook(sheets)},
		{"xl/_rels/workbook.xml.rels", workbookRels(len(sheets))},
		{"xl/styles.xml", styles},
	}
	for i, s := range sheets {
		files = append(files, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML(s)})
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, f.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const rootRels = xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

func contentTypes(n int) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	b.WriteString(`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func workbook(sheets []Sheet) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, s := range sheets {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(sheetName(s.Name)), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func workbookRels(n int) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i)
	}
	fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, n+1)
	b.WriteString(`</Relationships>`)
	return b.String()
}

// styles は Style の順番どおりの cellXfs を持つ。金額の表示形式は 164 番（3桁区切り、マイナスは赤の ▲。値は数値のまま）。
const styles = xmlHeader + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
	`<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0;[Red]&quot;▲&quot;#,##0"/></numFmts>` +
	`<fonts count="4">` +
	`<font><sz val="11"/><name val="Yu Gothic"/><family val="2"/></font>` +
	`<font><b/><sz val="11"/><name val="Yu Gothic"/><family val="2"/></font>` +
	`<font><b/><sz val="14"/><name val="Yu Gothic"/><family val="2"/></font>` +
	`<font><sz val="10"/><color rgb="FF6B7280"/><name val="Yu Gothic"/><family val="2"/></font>` +
	`</fonts>` +
	`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
	`<fill><patternFill patternType="solid"><fgColor rgb="FFF1F5F9"/><bgColor indexed="64"/></patternFill></fill></fills>` +
	`<borders count="3">` +
	`<border><left/><right/><top/><bottom/><diagonal/></border>` +
	`<border><left/><right/><top/><bottom style="thin"><color rgb="FFCBD5E1"/></bottom><diagonal/></border>` +
	`<border><left/><right/><top style="thin"><color rgb="FF64748B"/></top><bottom/><diagonal/></border>` +
	`</borders>` +
	`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs count="15">` +
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` + // Plain
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` + // Bold
	`<xf numFmtId="0" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>` + // Header
	`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` + // Number
	`<xf numFmtId="164" fontId="1" fillId="0" borderId="2" xfId="0" applyNumberFormat="1" applyFont="1" applyBorder="1"/>` + // NumberSum
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="2" xfId="0" applyFont="1" applyBorder="1"/>` + // LabelSum
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment indent="1"/></xf>` + // Indent1
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment indent="2"/></xf>` + // Indent2
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment indent="3"/></xf>` + // Indent3
	`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf>` + // Wrap
	`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>` + // Title
	`<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/>` + // Muted
	`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` + // NumberBold
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment indent="1"/></xf>` + // BoldIndent1
	`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1" applyAlignment="1"><alignment indent="2"/></xf>` + // BoldIndent2
	`</cellXfs>` +
	`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` +
	`</styleSheet>`

func sheetXML(s Sheet) string {
	var b strings.Builder
	b.WriteString(xmlHeader + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	// 要素の順番は仕様で決まっている: sheetViews → sheetFormatPr → cols → sheetData → mergeCells
	if s.FreezeRows > 0 || s.FreezeCols > 0 {
		pane := ""
		top := CellRef(s.FreezeRows, s.FreezeCols)
		active := "bottomRight"
		switch {
		case s.FreezeRows > 0 && s.FreezeCols > 0:
			pane = fmt.Sprintf(`<pane xSplit="%d" ySplit="%d" topLeftCell="%s" activePane="bottomRight" state="frozen"/>`, s.FreezeCols, s.FreezeRows, top)
		case s.FreezeRows > 0:
			pane = fmt.Sprintf(`<pane ySplit="%d" topLeftCell="%s" activePane="bottomLeft" state="frozen"/>`, s.FreezeRows, top)
			active = "bottomLeft"
		default:
			pane = fmt.Sprintf(`<pane xSplit="%d" topLeftCell="%s" activePane="topRight" state="frozen"/>`, s.FreezeCols, top)
			active = "topRight"
		}
		fmt.Fprintf(&b, `<sheetViews><sheetView workbookViewId="0">%s<selection pane="%s" activeCell="%s" sqref="%s"/></sheetView></sheetViews>`, pane, active, top, top)
	} else {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"/></sheetViews>`)
	}
	b.WriteString(`<sheetFormatPr defaultRowHeight="18"/>`)
	if len(s.Widths) > 0 {
		b.WriteString(`<cols>`)
		for i, w := range s.Widths {
			if w > 0 {
				fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w)
			}
		}
		b.WriteString(`</cols>`)
	}
	b.WriteString(`<sheetData>`)
	for r, row := range s.Rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cell := range row {
			ref := CellRef(r, c)
			switch {
			case cell.Num != nil:
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%s</v></c>`, ref, cell.Style, cell.Num.String())
			case cell.Text != "":
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, cell.Style, esc(cell.Text))
			case cell.Style != Plain:
				fmt.Fprintf(&b, `<c r="%s" s="%d"/>`, ref, cell.Style)
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	if len(s.Merges) > 0 {
		fmt.Fprintf(&b, `<mergeCells count="%d">`, len(s.Merges))
		for _, m := range s.Merges {
			fmt.Fprintf(&b, `<mergeCell ref="%s:%s"/>`, CellRef(m.Row, m.Col), CellRef(m.Row+m.Rows-1, m.Col+m.Cols-1))
		}
		b.WriteString(`</mergeCells>`)
	}
	b.WriteString(`<pageMargins left="0.7" right="0.7" top="0.75" bottom="0.75" header="0.3" footer="0.3"/>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

// CellRef は 0 始まりの行・列を「A1」の形にする。
func CellRef(row, col int) string {
	name := ""
	for c := col + 1; c > 0; c = (c - 1) / 26 {
		name = string(rune('A'+(c-1)%26)) + name
	}
	return fmt.Sprintf("%s%d", name, row+1)
}

// sheetName はシート名に使えない文字を除き、31文字までにする。
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '_'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	return s
}

// esc は XML の文字をエスケープする。XML に入れられない制御文字は除く。
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r':
			// 除く
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
