// Package httpx は JSON API のリクエスト・レスポンスの共通処理を提供する。
//
// エラーレスポンスは次の形式で返す:
//
//	{"error": {"code": "validation_error", "message": "...", "details": {"field": "..."}}}
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

const maxBodyBytes = 1 << 20 // 1MB

// Error は API のエラーレスポンス。
type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// BadRequest はリクエストの形式が不正なときのエラー。
func BadRequest(msg string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

// Validation は入力値の検証エラー。details はフィールド名 → メッセージ。
func Validation(details map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_error", Message: "入力内容に誤りがあります", Details: details}
}

// Unauthorized は未ログインまたは認証失敗のエラー。
func Unauthorized(msg string) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

// Forbidden は権限不足のエラー。
func Forbidden() *Error {
	return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "この操作を行う権限がありません"}
}

// NotFound は対象が存在しないときのエラー。
func NotFound(msg string) *Error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}

// Conflict は一意制約違反や参照中のデータ削除など、現在の状態と矛盾するときのエラー。
func Conflict(msg string) *Error {
	return &Error{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

// WriteJSON は v を JSON で書き出す。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "error", err)
	}
}

// WriteError は err をエラーレスポンスとして書き出す。
// *Error 以外のエラーは 500 とし、詳細はログにのみ出力する。
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		slog.ErrorContext(r.Context(), "internal error", "method", r.Method, "path", r.URL.Path, "error", err)
		apiErr = &Error{Status: http.StatusInternalServerError, Code: "internal_error", Message: "サーバーでエラーが発生しました"}
	}
	WriteJSON(w, apiErr.Status, map[string]*Error{"error": apiErr})
}

// HandlerFunc はエラーを返せる http.HandlerFunc。
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle は HandlerFunc を http.HandlerFunc に変換し、返されたエラーを書き出す。
func Handle(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// DecodeJSON はリクエストボディを v にデコードする。未知のフィールドはエラーにする。
// ボディが空の場合は v をそのままにして nil を返す。
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "リクエストが大きすぎます"}
		}
		return BadRequest(fmt.Sprintf("リクエストの JSON が不正です: %v", err))
	}
	if dec.More() {
		return BadRequest("リクエストの JSON が不正です: 複数の値が含まれています")
	}
	return nil
}

// PathID はパスパラメーター name を正の整数として取り出す。
func PathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, NotFound("指定されたデータが見つかりません")
	}
	return id, nil
}
