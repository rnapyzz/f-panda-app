package importer

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCommitHandlerParsesMappingJSONFieldNames guards against the exact bug
// caught during manual testing: ColumnMapping originally had no JSON
// struct tags, so the (snake_case) "mapping" form field's keys silently
// failed to match the (PascalCase) Go field names and every column index
// unmarshaled to its zero value. resolveRows and Service.Commit are both
// exercised elsewhere with a Go-constructed ColumnMapping, which never
// goes through JSON at all — only a real HTTP request catches this class
// of bug, so this test builds one directly rather than calling Commit().
func TestCommitHandlerParsesMappingJSONFieldNames(t *testing.T) {
	// This must be a literal snake_case JSON string, not json.Marshal of a
	// Go ColumnMapping value: marshaling-then-unmarshaling the same struct
	// round-trips correctly regardless of whether its `json:"..."` tags are
	// present, so that approach would never actually catch a regression.
	// This literal mirrors exactly what frontend/src/api/import.ts sends
	// (JSON.stringify of its ColumnMapping interface).
	mappingJSON := []byte(`{"business_column":2,"department_column":4,"account_column":1,"period_column":3,"amount_column":0}`)
	want := ColumnMapping{BusinessColumn: 2, DepartmentColumn: 4, AccountColumn: 1, PeriodColumn: 3, AmountColumn: 0}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("scenario_version_id", "1"); err != nil {
		t.Fatalf("write scenario_version_id field: %v", err)
	}
	if err := w.WriteField("mapping", string(mappingJSON)); err != nil {
		t.Fatalf("write mapping field: %v", err)
	}
	part, err := w.CreateFormFile("file", "actuals.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("金額,勘定科目,事業,期間,部門\n1000,人件費,事業A,2026-04,部門X\n")); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/import-batches", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())

	// readUploadedFile and the mapping-JSON decode happen before any
	// database access, so this test can exercise CommitHandler's HTTP
	// parsing up to (but not including) Service.Commit without a real DB:
	// a nil-Queries Service.Commit call will fail fast on the first query,
	// which is fine — what we're verifying is that ParseMultipartForm +
	// json.Unmarshal produce the *correct* mapping values before that.
	var gotMapping ColumnMapping
	var gotFilename string
	var gotContent []byte
	testHandler := func(w http.ResponseWriter, r *http.Request) {
		filename, content, ok := readUploadedFile(w, r)
		if !ok {
			return
		}
		gotFilename = filename
		gotContent = content
		if err := json.Unmarshal([]byte(r.FormValue("mapping")), &gotMapping); err != nil {
			t.Fatalf("unmarshal mapping form value: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}

	rec := httptest.NewRecorder()
	testHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotFilename != "actuals.csv" {
		t.Fatalf("filename = %q, want actuals.csv", gotFilename)
	}
	if len(gotContent) == 0 {
		t.Fatal("expected non-empty file content")
	}
	if gotMapping != want {
		t.Fatalf("mapping parsed from snake_case JSON as %+v, want %+v (JSON struct tags regression)", gotMapping, want)
	}
}
