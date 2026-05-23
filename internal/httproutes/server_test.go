package httproutes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStandaloneServeHTTPStripsSiloHeaders(t *testing.T) {
	srv := NewServer()
	srv.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Silo-User-Role") != "" {
			t.Fatalf("forged Silo header reached handler")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Silo-User-Role", "admin")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
