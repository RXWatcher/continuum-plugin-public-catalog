package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

const testSecret = "01234567890123456789012345678901"

type fakeHost struct {
	statsCalls int32
	mediaReq   runtimehost.ListLibraryMediaRequest
}

func (f *fakeHost) ListLibraryMedia(_ context.Context, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error) {
	f.mediaReq = req
	return &runtimehost.ListLibraryMediaResponse{}, nil
}

func (f *fakeHost) GetCatalogStats(context.Context, []string) (*runtimehost.CatalogStats, error) {
	atomic.AddInt32(&f.statsCalls, 1)
	return &runtimehost.CatalogStats{TotalItems: 12}, nil
}

func (f *fakeHost) CallPluginHTTP(context.Context, runtimehost.CallPluginHTTPRequest) (*runtimehost.CallPluginHTTPResponse, error) {
	return &runtimehost.CallPluginHTTPResponse{StatusCode: http.StatusNotFound}, nil
}

func (f *fakeHost) CallPluginJSON(context.Context, runtimehost.CallPluginJSONRequest) error {
	return nil
}

func TestCatalogMediaRejectsUnavailableMediaType(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:                func() Host { return host },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})
	token, err := signToken(testSecret, tokenClaims{
		Scope:      "catalog",
		ExpiresAt:  time.Now().Add(time.Hour).Unix(),
		MediaTypes: []string{"movie"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=ebook", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if host.mediaReq.MediaTypes != nil {
		t.Fatalf("host was called with media types: %#v", host.mediaReq.MediaTypes)
	}
}

func TestCatalogMediaKeepsTokenMediaTypeScope(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:                func() Host { return host },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})
	token, err := signToken(testSecret, tokenClaims{
		Scope:      "catalog",
		ExpiresAt:  time.Now().Add(time.Hour).Unix(),
		MediaTypes: []string{"movie"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := strings.Join(host.mediaReq.MediaTypes, ","); got != "movie" {
		t.Fatalf("media types = %q, want movie", got)
	}
}

func TestSecurityHeadersAndCatalogNoStore(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})
	token, err := signToken(testSecret, tokenClaims{
		Scope:     "catalog",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/catalog?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestStatsEndpointUsesShortCache(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:                func() Host { return host },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		StatsCacheTTL:       time.Minute,
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/public/stats", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
	if got := atomic.LoadInt32(&host.statsCalls); got != 1 {
		t.Fatalf("stats calls = %d, want 1", got)
	}
}
