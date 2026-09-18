package importer

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func TestDetectAndParseCSVUTF8(t *testing.T) {
	content := []byte("事業,部門,勘定科目,期間,金額\n事業A,部門X,人件費,2026-04,1000000\n")
	table, err := DetectAndParse("actuals.csv", content)
	if err != nil {
		t.Fatalf("DetectAndParse: %v", err)
	}
	want := []string{"事業", "部門", "勘定科目", "期間", "金額"}
	if !equalStrings(table.Headers, want) {
		t.Fatalf("headers = %v, want %v", table.Headers, want)
	}
	if len(table.Rows) != 1 || table.Rows[0][0] != "事業A" {
		t.Fatalf("unexpected rows: %v", table.Rows)
	}
}

func TestDetectAndParseCSVWithUTF8BOM(t *testing.T) {
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("a,b\n1,2\n")...)
	table, err := DetectAndParse("f.csv", content)
	if err != nil {
		t.Fatalf("DetectAndParse: %v", err)
	}
	if table.Headers[0] != "a" {
		t.Fatalf("BOM was not stripped, got header %q", table.Headers[0])
	}
}

func TestDetectAndParseCSVShiftJIS(t *testing.T) {
	utf8Content := "事業,部門,勘定科目,期間,金額\n事業A,部門X,人件費,2026-04,1000000\n"
	sjis, _, err := transform.Bytes(japanese.ShiftJIS.NewEncoder(), []byte(utf8Content))
	if err != nil {
		t.Fatalf("encode fixture to Shift-JIS: %v", err)
	}

	table, err := DetectAndParse("actuals.csv", sjis)
	if err != nil {
		t.Fatalf("DetectAndParse: %v", err)
	}
	if table.Headers[0] != "事業" {
		t.Fatalf("Shift-JIS was not decoded correctly, got header %q", table.Headers[0])
	}
	if table.Rows[0][0] != "事業A" {
		t.Fatalf("unexpected row: %v", table.Rows[0])
	}
}

func TestDetectAndParseXLSX(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	_ = f.SetSheetRow(sheet, "A1", &[]any{"事業", "部門", "勘定科目", "期間", "金額"})
	_ = f.SetSheetRow(sheet, "A2", &[]any{"事業A", "部門X", "人件費", "2026-04", 1000000})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write fixture xlsx: %v", err)
	}

	table, err := DetectAndParse("actuals.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("DetectAndParse: %v", err)
	}
	if table.Headers[0] != "事業" {
		t.Fatalf("unexpected headers: %v", table.Headers)
	}
	if len(table.Rows) != 1 || table.Rows[0][0] != "事業A" {
		t.Fatalf("unexpected rows: %v", table.Rows)
	}
}

func TestDetectAndParseRejectsExtensionContentMismatch(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write fixture xlsx: %v", err)
	}

	// A real XLSX (ZIP-signed) uploaded with a .csv name must be rejected —
	// this is exactly the "declared type doesn't match actual content"
	// check that stops a crafted upload from confusing the wrong parser.
	_, err := DetectAndParse("actuals.csv", buf.Bytes())
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}

func TestDetectAndParseRejectsCSVNamedFileThatIsActuallyBinary(t *testing.T) {
	_, err := DetectAndParse("actuals.csv", []byte("PK\x03\x04 not really a csv"))
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}

func TestDetectAndParseRejectsUnsupportedExtension(t *testing.T) {
	_, err := DetectAndParse("actuals.txt", []byte("a,b\n1,2\n"))
	if !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected ErrUnsupportedFileType, got %v", err)
	}
}

func TestDetectAndParseRejectsEmptyFile(t *testing.T) {
	_, err := DetectAndParse("actuals.csv", nil)
	if !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("expected ErrEmptyFile, got %v", err)
	}
}

func TestParseCSVRejectsTooManyRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("a,b\n")
	for i := 0; i < maxRows+10; i++ {
		b.WriteString("1,2\n")
	}
	_, err := DetectAndParse("big.csv", []byte(b.String()))
	if !errors.Is(err, ErrTooManyRows) {
		t.Fatalf("expected ErrTooManyRows, got %v", err)
	}
}

// TestXLSXUnzipSizeLimitIsEnforced verifies the zip-bomb safety net is
// actually wired up: excelize.Options.UnzipSizeLimit must be small enough
// that even opening a completely normal, tiny XLSX fails when the limit is
// set below its unzipped size. This tests that OUR code passes the limit
// through correctly, rather than trying to fabricate a real pathological
// zip-bomb fixture (excelize's own test suite already covers that the
// underlying mechanism resists one).
func TestXLSXUnzipSizeLimitIsEnforced(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	_ = f.SetSheetRow(sheet, "A1", &[]any{"a", "b", "c"})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write fixture xlsx: %v", err)
	}

	// Sanity check: parses fine with the real (generous) limits.
	if _, err := parseXLSXWithLimits(buf.Bytes(), maxDecompressedBytes, maxUnzipXMLBytes); err != nil {
		t.Fatalf("expected fixture to parse with normal limits: %v", err)
	}

	// With a 1-byte limit, even this tiny file must be rejected.
	if _, err := parseXLSXWithLimits(buf.Bytes(), 1, 1); err == nil {
		t.Fatal("expected an error when UnzipSizeLimit is far smaller than the file, got nil")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
