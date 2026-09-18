// Package importer implements security-hardened CSV/XLSX parsing for the
// actuals import feature, plus the column-mapping resolution that turns a
// parsed table into fact_amount rows. This is the largest attack surface in
// the app — an uploaded file is fully untrusted input — so every step here
// is bounded: file size, decompressed size, entry count, and parse time.
package importer

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	// MaxUploadBytes bounds the raw (compressed, as received) upload size.
	MaxUploadBytes = 20 << 20 // 20MB

	// maxDecompressedBytes bounds an XLSX's total unzipped size, so a
	// small-but-adversarial file (a zip bomb) can't exhaust memory even
	// though it passes the raw upload size check above.
	maxDecompressedBytes = 200 << 20 // 200MB
	maxUnzipXMLBytes     = 50 << 20  // 50MB per worksheet/shared-string part

	// parseTimeout bounds worst-case CPU time for a single parse, in case a
	// crafted file is small and within the size limits but pathologically
	// slow to process.
	parseTimeout = 20 * time.Second

	// maxRows caps how many data rows we'll ever process from one file,
	// independent of the byte-size limits above.
	maxRows = 200_000
)

var (
	ErrUnsupportedFileType = errors.New("importer: unsupported file type (expected .csv or .xlsx)")
	ErrTooManyRows         = errors.New("importer: file has too many rows")
	ErrParseTimeout        = errors.New("importer: parsing took too long")
	ErrEmptyFile           = errors.New("importer: file is empty")
)

// ParsedTable is the format-agnostic result of parsing either a CSV or an
// XLSX file: everything downstream (preview, column mapping) works off this
// regardless of which parser produced it.
type ParsedTable struct {
	Headers []string
	Rows    [][]string
}

// xlsxSignature is the ZIP local file header magic bytes; XLSX is a ZIP
// container, so this is the standard way to verify the actual file content
// matches its claimed type rather than trusting the filename/content-type.
var xlsxSignature = []byte("PK\x03\x04")

// DetectAndParse sniffs the file's real type from its content (not the
// filename) and dispatches to the matching parser. filename is used only to
// require a plausible matching extension as a secondary check.
func DetectAndParse(filename string, content []byte) (ParsedTable, error) {
	if len(content) == 0 {
		return ParsedTable{}, ErrEmptyFile
	}

	lowerName := strings.ToLower(filename)
	isXLSXByContent := bytes.HasPrefix(content, xlsxSignature)

	switch {
	case isXLSXByContent:
		if !strings.HasSuffix(lowerName, ".xlsx") {
			return ParsedTable{}, fmt.Errorf("%w: content looks like an XLSX file but the name doesn't end in .xlsx", ErrUnsupportedFileType)
		}
		return parseWithTimeout(func() (ParsedTable, error) { return parseXLSX(content) })
	case strings.HasSuffix(lowerName, ".csv"):
		// A CSV must not start with the ZIP signature — that would mean the
		// content doesn't actually match a plain-text CSV.
		if bytes.HasPrefix(content, []byte("PK")) {
			return ParsedTable{}, fmt.Errorf("%w: content does not look like a CSV file", ErrUnsupportedFileType)
		}
		return parseWithTimeout(func() (ParsedTable, error) { return parseCSV(content) })
	default:
		return ParsedTable{}, ErrUnsupportedFileType
	}
}

func parseWithTimeout(fn func() (ParsedTable, error)) (ParsedTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), parseTimeout)
	defer cancel()

	type result struct {
		table ParsedTable
		err   error
	}
	done := make(chan result, 1)
	go func() {
		table, err := fn()
		done <- result{table, err}
	}()

	select {
	case r := <-done:
		return r.table, r.err
	case <-ctx.Done():
		return ParsedTable{}, ErrParseTimeout
	}
}

func parseCSV(content []byte) (ParsedTable, error) {
	decoded, err := decodeToUTF8(content)
	if err != nil {
		return ParsedTable{}, fmt.Errorf("decode CSV text: %w", err)
	}

	reader := csv.NewReader(strings.NewReader(decoded))
	reader.FieldsPerRecord = -1 // tolerate ragged rows; mapping resolution will flag short rows
	reader.LazyQuotes = true

	var all [][]string
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ParsedTable{}, fmt.Errorf("parse CSV: %w", err)
		}
		all = append(all, record)
		if len(all) > maxRows {
			return ParsedTable{}, ErrTooManyRows
		}
	}
	if len(all) == 0 {
		return ParsedTable{}, ErrEmptyFile
	}
	return ParsedTable{Headers: all[0], Rows: all[1:]}, nil
}

// decodeToUTF8 handles the common case of Shift-JIS-encoded CSV exports
// from Japanese accounting systems: valid UTF-8 is passed through as-is
// (after stripping a BOM if present), anything else is assumed to be
// Shift-JIS and transcoded.
func decodeToUTF8(content []byte) (string, error) {
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	if utf8.Valid(content) {
		return string(content), nil
	}
	out, _, err := transform.Bytes(japanese.ShiftJIS.NewDecoder(), content)
	if err != nil {
		return "", fmt.Errorf("not valid UTF-8 or Shift-JIS: %w", err)
	}
	return string(out), nil
}

func parseXLSX(content []byte) (ParsedTable, error) {
	return parseXLSXWithLimits(content, maxDecompressedBytes, maxUnzipXMLBytes)
}

// parseXLSXWithLimits is parseXLSX with the size limits as parameters,
// purely so tests can verify the limits are actually wired up (by passing
// an artificially tiny limit against a normal file) without needing to
// construct a real pathological zip bomb fixture.
func parseXLSXWithLimits(content []byte, unzipSizeLimit, unzipXMLSizeLimit int64) (ParsedTable, error) {
	f, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{
		UnzipSizeLimit:    unzipSizeLimit,
		UnzipXMLSizeLimit: unzipXMLSizeLimit,
	})
	if err != nil {
		return ParsedTable{}, fmt.Errorf("open XLSX: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return ParsedTable{}, ErrEmptyFile
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return ParsedTable{}, fmt.Errorf("read XLSX rows: %w", err)
	}
	if len(rows) == 0 {
		return ParsedTable{}, ErrEmptyFile
	}
	if len(rows) > maxRows {
		return ParsedTable{}, ErrTooManyRows
	}
	return ParsedTable{Headers: rows[0], Rows: rows[1:]}, nil
}
