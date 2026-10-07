// Package csvio はマスタ・施策の CSV インポート・エクスポートに共通する処理を提供する。
//
// インポートは「追加と更新」のみ（CSV にない行は削除しない）。1行でもエラーがあれば何も保存せず、
// 行番号付きのエラーを返す。dry_run=true なら検証と件数の集計だけを行う。
// エクスポートは Excel でそのまま開けるよう、BOM 付きの UTF-8 で書き出す。
package csvio

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rnapyzz/f-panda-app/api/internal/httpx"
)

const (
	maxUploadBytes = 20 << 20 // 20MB
	maxRows        = 50_000
	maxRowErrors   = 100
)

var bom = []byte("\xef\xbb\xbf")

// Upload は取込リクエストの内容。
type Upload struct {
	Data   []byte
	Reason string
	DryRun bool
}

// ReadUpload は multipart/form-data の file と reason を読み込む。本番の取込（dry run 以外）は理由が必須。
func ReadUpload(w http.ResponseWriter, r *http.Request) (Upload, error) {
	up := Upload{DryRun: r.URL.Query().Get("dry_run") == "true"}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return up, &httpx.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "ファイルは 20MB 以下にしてください"}
		}
		return up, httpx.BadRequest("multipart/form-data で file と reason を送ってください")
	}
	defer r.MultipartForm.RemoveAll()

	up.Reason = strings.TrimSpace(r.FormValue("reason"))
	if up.Reason == "" && !up.DryRun {
		return up, httpx.Validation(map[string]string{"reason": "変更理由を入力してください"})
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return up, httpx.Validation(map[string]string{"file": "CSV ファイルを選択してください"})
	}
	defer file.Close()
	if up.Data, err = io.ReadAll(file); err != nil {
		return up, err
	}
	return up, nil
}

// Row は CSV の1データ行。
type Row struct {
	Line   int
	values map[string]string
}

// Has は列がヘッダーにあるかを返す（省略できる列の判定に使う）。
func (r Row) Has(column string) bool {
	_, ok := r.values[column]
	return ok
}

// Get は列の値を前後の空白を除いて返す。
func (r Row) Get(column string) string {
	return strings.TrimSpace(r.values[column])
}

// Parse は CSV を読み込み、ヘッダーが columns と一致すること（順番は問わない）を確認して行を返す。
func Parse(data []byte, columns []string) ([]Row, error) {
	return ParseWithOptional(data, columns, nil)
}

// ParseWithOptional は Parse と同じだが、optional の列はヘッダーになくてもよい（値は空として読む）。
func ParseWithOptional(data []byte, columns, optional []string) ([]Row, error) {
	data = bytes.TrimPrefix(data, bom)
	if !utf8.Valid(data) {
		return nil, httpx.BadRequest("CSV の文字コードが UTF-8 ではありません。UTF-8 で保存してください")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, httpx.BadRequest("CSV が空です")
	}
	if err != nil {
		return nil, httpx.BadRequest("CSV のヘッダー行を読み込めません: " + err.Error())
	}
	index := map[string]int{}
	for i, h := range header {
		index[strings.ToLower(strings.TrimSpace(h))] = i
	}
	headerError := "CSV のヘッダー行は " + strings.Join(columns, ",") + " にしてください（順番は自由）"
	if len(optional) > 0 {
		headerError = "CSV のヘッダー行は " + strings.Join(columns, ",") + "（必須）と " + strings.Join(optional, ",") + "（省略可）にしてください（順番は自由）"
	}
	present := 0
	for _, c := range columns {
		if _, ok := index[c]; !ok {
			return nil, httpx.BadRequest(headerError)
		}
		present++
	}
	all := append(append([]string{}, columns...), optional...)
	for _, c := range optional {
		if _, ok := index[c]; ok {
			present++
		}
	}
	if len(header) != present || len(index) != len(header) {
		return nil, httpx.BadRequest(headerError)
	}
	r.FieldsPerRecord = len(header)

	var rows []Row
	errs := &RowErrors{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var parseErr *csv.ParseError
		if errors.As(err, &parseErr) {
			if errors.Is(parseErr.Err, csv.ErrFieldCount) {
				errs.Add(parseErr.Line, "列の数が %d ではありません", len(header))
				continue
			}
			errs.Add(parseErr.Line, "CSV の形式が正しくありません: %v", parseErr.Err)
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := r.FieldPos(0)
		if len(rows) >= maxRows {
			errs.Add(line, "データ行は %d 行までです", maxRows)
			break
		}
		values := map[string]string{}
		for _, c := range all {
			if i, ok := index[c]; ok {
				values[c] = rec[i]
			}
		}
		rows = append(rows, Row{Line: line, values: values})
	}
	if err := errs.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, httpx.BadRequest("CSV にデータ行がありません")
	}
	return rows, nil
}

// RowErrors は行ごとのエラーを上限まで集める。
type RowErrors struct {
	items []httpx.RowError
	total int
}

// Add は行のエラーを追加する。
func (e *RowErrors) Add(line int, format string, args ...any) {
	e.total++
	if len(e.items) < maxRowErrors {
		e.items = append(e.items, httpx.RowError{Line: line, Message: fmt.Sprintf(format, args...)})
	}
}

// AddDetails は検証エラー（項目 → メッセージ）を行のエラーとして追加する。
func (e *RowErrors) AddDetails(line int, err error) {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) && len(apiErr.Details) > 0 {
		for _, msg := range apiErr.Details {
			e.Add(line, "%s", msg)
		}
		return
	}
	e.Add(line, "%v", err)
}

// Empty はエラーがないかを返す。
func (e *RowErrors) Empty() bool { return e.total == 0 }

// Err はエラーがあれば 422（code: invalid_csv）を返す。
func (e *RowErrors) Err() error {
	if e.total == 0 {
		return nil
	}
	msg := fmt.Sprintf("CSV に %d 件のエラーがあるため取り込みませんでした", e.total)
	if e.total > len(e.items) {
		msg += fmt.Sprintf("（先頭の %d 件を表示）", len(e.items))
	}
	return &httpx.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_csv", Message: msg, Rows: e.items}
}

// Result は取込の結果。
type Result struct {
	DryRun    bool `json:"dry_run"`
	Rows      int  `json:"rows"`
	Inserted  int  `json:"inserted"`
	Updated   int  `json:"updated"`
	Unchanged int  `json:"unchanged"`
	// Warnings は取り込んだうえでの注意（例: 担当が残っている無効なユーザー）
	Warnings []string `json:"warnings,omitempty"`
}

// ErrDryRun は dry run でトランザクションをロールバックさせるためのエラー。
var ErrDryRun = errors.New("dry run")

// Finish は取込の結果を書き出す。dry run のロールバック（ErrDryRun）は成功として扱う。
func Finish(w http.ResponseWriter, result Result, err error) error {
	if err != nil && !errors.Is(err, ErrDryRun) {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

// WriteCSV は BOM 付き UTF-8 の CSV を添付ファイルとして書き出す。ファイル名は "<name>-YYYYMMDD.csv"。
func WriteCSV(w http.ResponseWriter, name string, header []string, rows [][]string) error {
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(bom); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true // Excel との相性のため
	if err := cw.Write(header); err != nil {
		return err
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	return cw.Error()
}
