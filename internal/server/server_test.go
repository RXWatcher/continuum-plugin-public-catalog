package server

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestCatalogPageUsesProxyRelativeAPIURL(t *testing.T) {
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `fetch("api/catalog/media?"`) {
		t.Fatalf("catalog page should fetch media through a relative plugin URL")
	}
	if strings.Contains(body, `fetch("/api/catalog/media?`) {
		t.Fatalf("catalog page should not fetch from the host root")
	}
}

func TestAdminPageUsesProxyRelativeAPIURL(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `fetch("api/admin/catalog-token"`) {
		t.Fatalf("admin page should fetch token generation through a relative plugin URL")
	}
	if !strings.Contains(body, `h.Authorization="Bearer "+hostToken`) {
		t.Fatalf("admin page should forward the host token on admin API calls")
	}
	if !strings.Contains(body, `history.replaceState`) {
		t.Fatalf("admin page should strip the host token from the address bar after capture")
	}
	for _, want := range []string{"Generate Link", "Media types", "Library IDs", "Refresh stats"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}
	if strings.Contains(body, `fetch("/api/admin/catalog-token"`) {
		t.Fatalf("admin page should not fetch from the host root")
	}
}

func TestNilHostStatsReturnsEmptyPayloadInsteadOfPanicking(t *testing.T) {
	h := New(Deps{
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/public/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	token, err := signToken(testSecret, tokenClaims{
		Scope:     "catalog",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("media status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Items":[]`) {
		t.Fatalf("media body should be empty payload, got %s", rec.Body.String())
	}
}

func TestCreateTokenRejectsMalformedJSON(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString("{"))
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateTokenDefaultsAndCapsTTL(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 0,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"hours":999999}`))
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	start := time.Now().UTC()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Token     string `json:"token"`
		URL       string `json:"url"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Token == "" || body.URL == "" {
		t.Fatalf("token response missing token/url: %+v", body)
	}
	if strings.HasPrefix(body.URL, "/") {
		t.Fatalf("relative plugin catalog URL must not start at host root: %q", body.URL)
	}
	if !strings.HasPrefix(body.URL, "catalog?token=") {
		t.Fatalf("catalog URL = %q, want plugin-relative catalog link", body.URL)
	}
	expiresAt, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expiresAt: %v", err)
	}
	max := start.Add(24 * 365 * time.Hour).Add(time.Minute)
	if expiresAt.After(max) {
		t.Fatalf("expiresAt = %s, want capped to at most one year", expiresAt)
	}
}

func TestCreateTokenAcceptsEmptyBodyWithDefaultTTL(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 2,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", http.NoBody)
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
