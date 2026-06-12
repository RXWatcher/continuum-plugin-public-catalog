package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
)

// paginatingConfigStore embeds the in-memory fake but serves a fixed
// store.CatalogMedia page carrying a plaintext "offset:N" NextPageToken,
// so handler-level page-token signing/verification can be exercised.
type paginatingConfigStore struct {
	*fakeConfigStore
	lastPageToken string
}

func (s *paginatingConfigStore) CatalogMedia(_ context.Context, q store.CatalogMediaQuery) (*store.CatalogMediaResponse, error) {
	s.lastPageToken = q.PageToken
	return &store.CatalogMediaResponse{
		Items:         []store.CatalogMediaItem{{MediaID: "m1", Title: "One"}},
		NextPageToken: "offset:48",
		TotalCount:    200,
	}, nil
}

func newPaginatingDeps() (Deps, *paginatingConfigStore) {
	cfg := &paginatingConfigStore{fakeConfigStore: newFakeConfigStore()}
	d := Deps{
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		ConfigStore:         cfg,
	}
	return d, cfg
}

func catalogToken(t *testing.T) string {
	t.Helper()
	token, err := signToken(testSecret, tokenClaims{
		Scope:      "catalog",
		ExpiresAt:  time.Now().Add(time.Hour).Unix(),
		MediaTypes: []string{"movie"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	return token
}

func TestCatalogMediaSignsNextPageToken(t *testing.T) {
	d, cfgStore := newPaginatingDeps()
	h := New(d)
	token := catalogToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=movie", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The wire token must NOT be the raw store cursor — it must be signed.
	if body.NextPageToken == "" {
		t.Fatal("expected a signed next page token")
	}
	if body.NextPageToken == "offset:48" {
		t.Fatal("next page token leaked the raw store cursor; expected a signed envelope")
	}
	inner, err := verifyPageToken(testSecret, body.NextPageToken)
	if err != nil {
		t.Fatalf("returned token should verify: %v", err)
	}
	if inner != "offset:48" {
		t.Fatalf("inner cursor = %q, want offset:48", inner)
	}
	// First request had no page token, so the store saw an empty cursor.
	if cfgStore.lastPageToken != "" {
		t.Fatalf("first page should pass empty cursor to store, got %q", cfgStore.lastPageToken)
	}
}

func TestCatalogMediaAcceptsSignedTokenAndForwardsInnerCursor(t *testing.T) {
	d, cfgStore := newPaginatingDeps()
	h := New(d)
	token := catalogToken(t)

	signed := signPageToken(testSecret, "offset:48")
	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=movie&page_token="+signed, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if cfgStore.lastPageToken != "offset:48" {
		t.Fatalf("store cursor = %q, want offset:48 decoded from signed token", cfgStore.lastPageToken)
	}
}

func TestCatalogMediaRejectsHandCraftedPageToken(t *testing.T) {
	d, _ := newPaginatingDeps()
	h := New(d)
	token := catalogToken(t)

	// A plaintext "offset:9999" with no valid signature must be rejected,
	// so offsets can't be hand-crafted to enumerate the result set.
	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=movie&page_token=offset:9999", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_page_token") {
		t.Fatalf("body should flag invalid page token, got %s", rec.Body.String())
	}
}

func TestCatalogMediaRejectsWrongSecretSignature(t *testing.T) {
	d, _ := newPaginatingDeps()
	h := New(d)
	token := catalogToken(t)

	// Validly-structured token signed with a different secret must fail.
	forged := signPageToken("a-totally-different-secret-value-32b", "offset:48")
	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=movie&page_token="+forged, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestVerifyPageTokenEnforcesAbsoluteOffsetCap(t *testing.T) {
	// Even a correctly-signed token past the absolute cap is rejected.
	tooDeep := signPageToken(testSecret, "offset:"+strconv.Itoa(maxCatalogOffset+1))
	if _, err := verifyPageToken(testSecret, tooDeep); err == nil {
		t.Fatal("expected over-cap signed token to be rejected")
	}
	atCap := signPageToken(testSecret, "offset:"+strconv.Itoa(maxCatalogOffset))
	if _, err := verifyPageToken(testSecret, atCap); err != nil {
		t.Fatalf("token at the cap should verify, got %v", err)
	}
}

func TestVerifyPageTokenEmptyIsFirstPage(t *testing.T) {
	inner, err := verifyPageToken(testSecret, "")
	if err != nil {
		t.Fatalf("empty token should be valid: %v", err)
	}
	if inner != "" {
		t.Fatalf("empty token inner = %q, want empty", inner)
	}
}

func TestPublicStatsSetsShortPublicCacheForAnonymous(t *testing.T) {
	d, _, _ := newTestDeps()
	d.StatsCacheTTL = 30 * time.Second
	h := New(d)

	req := httptest.NewRequest(http.MethodGet, "/api/public/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "public") || !strings.Contains(cc, "max-age=30") {
		t.Fatalf("Cache-Control = %q, want public max-age=30 for anonymous stats", cc)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Cookie") {
		t.Fatalf("Vary = %q, want to include Cookie", vary)
	}
}

func TestPublicStatsKeepsNoStoreForSessionBearingRequest(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)
	token := catalogToken(t)

	req := httptest.NewRequest(http.MethodGet, "/api/public/stats?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store for session-bearing stats request", cc)
	}
}

func TestSessionScopedLandingStaysNoStore(t *testing.T) {
	// The catalog page is session-gated and must never get a shared-cache
	// directive even though it embeds stats.
	d, _, _ := newTestDeps()
	h := New(d)
	token := catalogToken(t)

	req := httptest.NewRequest(http.MethodGet, "/catalog?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store for session-gated catalog page", cc)
	}
}
