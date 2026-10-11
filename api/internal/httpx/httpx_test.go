package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleErrorAfterResponseStarted(t *testing.T) {
	// 書き出しを始めた後のエラーは、本文に JSON を付け足さない
	h := Handle(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("a,b\n"))
		return errors.New("broken pipe")
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "a,b\n" {
		t.Errorf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	// 書き出す前のエラーは JSON のエラー
	h = Handle(func(w http.ResponseWriter, r *http.Request) error { return NotFound("ない") })
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("status = %d, content-type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
