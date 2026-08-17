package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeCreateRejectsTrailingJSON(t *testing.T) {
	s := New()
	req := httptest.NewRequest(http.MethodPost, "/hll/one", bytes.NewBufferString(`{"precision":10} {"precision":10}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
