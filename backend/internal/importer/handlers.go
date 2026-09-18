package importer

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/httpx"
)

// readUploadedFile enforces MaxUploadBytes on the request body before
// parsing the multipart form, and returns the uploaded file's bytes and
// original filename. Sized limits are applied before any parsing happens,
// so an oversized upload is rejected cheaply.
func readUploadedFile(w http.ResponseWriter, r *http.Request) (filename string, content []byte, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
		http.Error(w, "ファイルサイズが上限（20MB）を超えているか、リクエストが不正です", http.StatusBadRequest)
		return "", nil, false
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file フィールドが必要です", http.StatusBadRequest)
		return "", nil, false
	}
	defer file.Close()

	content, err = io.ReadAll(file)
	if err != nil {
		http.Error(w, "ファイルの読み込みに失敗しました", http.StatusBadRequest)
		return "", nil, false
	}
	return header.Filename, content, true
}

type previewResponseDTO struct {
	Headers    []string   `json:"headers"`
	SampleRows [][]string `json:"sample_rows"`
	TotalRows  int        `json:"total_rows"`
}

func PreviewHandler(w http.ResponseWriter, r *http.Request) {
	filename, content, ok := readUploadedFile(w, r)
	if !ok {
		return
	}

	result, err := Preview(filename, content)
	if err != nil {
		respondParseError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, previewResponseDTO{
		Headers:    result.Headers,
		SampleRows: result.SampleRows,
		TotalRows:  result.TotalRows,
	})
}

type commitResponseDTO struct {
	ImportBatchID int64      `json:"import_batch_id"`
	Status        string     `json:"status"`
	RowCount      int        `json:"row_count"`
	ErrorCount    int        `json:"error_count"`
	Errors        []RowError `json:"errors"`
}

func (s *Service) CommitHandler(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	filename, content, ok := readUploadedFile(w, r)
	if !ok {
		return
	}

	scenarioVersionID, err := strconv.ParseUint(r.FormValue("scenario_version_id"), 10, 64)
	if err != nil {
		http.Error(w, "scenario_version_id が不正です", http.StatusBadRequest)
		return
	}

	var mapping ColumnMapping
	if err := json.Unmarshal([]byte(r.FormValue("mapping")), &mapping); err != nil {
		http.Error(w, "mapping が不正です", http.StatusBadRequest)
		return
	}

	result, err := s.Commit(r.Context(), CommitInput{
		UploadedBy:        user.ID,
		OriginalFilename:  filename,
		FileSizeBytes:     len(content),
		ScenarioVersionID: scenarioVersionID,
		Content:           content,
		Mapping:           mapping,
	})
	if err != nil {
		if errors.Is(err, ErrScenarioNotActual) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrScenarioVersionLocked) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		respondParseError(w, err)
		return
	}

	errs := result.Errors
	if errs == nil {
		errs = []RowError{}
	}
	httpx.WriteJSON(w, http.StatusOK, commitResponseDTO{
		ImportBatchID: result.ImportBatchID,
		Status:        result.Status,
		RowCount:      result.RowCount,
		ErrorCount:    result.ErrorCount,
		Errors:        errs,
	})
}

type importBatchDTO struct {
	ID               uint64  `json:"id"`
	OriginalFilename string  `json:"original_filename"`
	FileSizeBytes    int32   `json:"file_size_bytes"`
	Status           string  `json:"status"`
	RowCount         int32   `json:"row_count"`
	ErrorCount       int32   `json:"error_count"`
	CreatedAt        string  `json:"created_at"`
	CompletedAt      *string `json:"completed_at,omitempty"`
}

func (s *Service) ListBatchesHandler(w http.ResponseWriter, r *http.Request) {
	scenarioVersionID, err := strconv.ParseUint(r.URL.Query().Get("scenario_version_id"), 10, 64)
	if err != nil {
		http.Error(w, "scenario_version_id query parameter is required", http.StatusBadRequest)
		return
	}

	rows, err := s.ListBatches(r.Context(), scenarioVersionID)
	if err != nil {
		slog.Error("list import batches failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	out := make([]importBatchDTO, len(rows))
	for i, row := range rows {
		dto := importBatchDTO{
			ID: row.ID, OriginalFilename: row.OriginalFilename, FileSizeBytes: row.FileSizeBytes,
			Status: string(row.Status), RowCount: row.RowCount, ErrorCount: row.ErrorCount,
			CreatedAt: row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if row.CompletedAt.Valid {
			s := row.CompletedAt.Time.Format("2006-01-02T15:04:05Z07:00")
			dto.CompletedAt = &s
		}
		out[i] = dto
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func respondParseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnsupportedFileType), errors.Is(err, ErrEmptyFile), errors.Is(err, ErrTooManyRows):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrParseTimeout):
		http.Error(w, err.Error(), http.StatusRequestTimeout)
	default:
		slog.Error("import parse error", "error", err)
		http.Error(w, "ファイルの解析に失敗しました", http.StatusBadRequest)
	}
}
