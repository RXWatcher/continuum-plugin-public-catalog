package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pluginrt "github.com/ContinuumApp/continuum-plugin-public-catalog/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/store"
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

func (f *fakeHost) ResolveCatalogImageURLs(context.Context, []string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (f *fakeHost) CallPluginHTTP(context.Context, runtimehost.CallPluginHTTPRequest) (*runtimehost.CallPluginHTTPResponse, error) {
	return &runtimehost.CallPluginHTTPResponse{StatusCode: http.StatusNotFound}, nil
}

func (f *fakeHost) CallPluginJSON(context.Context, runtimehost.CallPluginJSONRequest) error {
	return nil
}

type overlayHost struct {
	fakeHost
	resolved map[string]string
}

type memoryHTMLStore struct {
	html string
	err  error
}

func (m *memoryHTMLStore) LoadHTML(context.Context) (string, error) {
	return m.html, m.err
}

func (m *memoryHTMLStore) SaveHTML(_ context.Context, html string) error {
	m.html = html
	return m.err
}

type failingCatalogSource struct{}

func (f failingCatalogSource) MediaType() string { return "ebook" }
func (f failingCatalogSource) Stats(context.Context, Host) (*runtimehost.CatalogStats, error) {
	return nil, errors.New("upstream unavailable")
}
func (f failingCatalogSource) List(context.Context, Host, runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error) {
	return nil, errors.New("upstream unavailable")
}

type panickingStatsHost struct {
	fakeHost
}

func (p *panickingStatsHost) GetCatalogStats(context.Context, []string) (*runtimehost.CatalogStats, error) {
	panic("runtime host stats client unavailable")
}

func TestPublicStatsReturnsJSONWhenHostStatsPanics(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &panickingStatsHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/public/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("content-type = %q, want JSON", contentType)
	}
	var stats runtimehost.CatalogStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("stats response should be JSON: %v; body=%s", err, rec.Body.String())
	}
}

func (o *overlayHost) ResolveCatalogImageURLs(_ context.Context, paths []string, variant string) (map[string]string, error) {
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		out[path] = o.resolved[path]
	}
	return out, nil
}

func TestOverlayCatalogMediaImagesFillsMissingURLsFromResolvedPaths(t *testing.T) {
	t.Skip("requires store-backed image path lookup")
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

func TestCatalogMediaReportsSingleSourceFailure(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		Sources:             []CatalogSource{failingCatalogSource{}},
	})
	token, err := signToken(testSecret, tokenClaims{
		Scope:      "catalog",
		ExpiresAt:  time.Now().Add(time.Hour).Unix(),
		MediaTypes: []string{"ebook"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token+"&media_type=ebook", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "catalog_source_unavailable") {
		t.Fatalf("body should identify source failure, got %s", rec.Body.String())
	}
}

func TestScopeCatalogStatsFiltersToAllowedMediaTypes(t *testing.T) {
	stats := &store.CatalogStats{
		TotalItems: 15,
		MediaTypeCounts: []store.CatalogTypeCount{
			{MediaType: "movie", Count: 10},
			{MediaType: "series", Count: 5},
		},
		LibraryCounts: []store.CatalogLibraryCount{
			{LibraryID: "1", LibraryName: "Movies", MediaType: "movie", Count: 10},
			{LibraryID: "2", LibraryName: "TV Shows", MediaType: "tv", Count: 5},
		},
		QualityCounts: []store.CatalogQualityCount{{Key: "4k", Label: "4K", Count: 7}},
	}

	scoped := scopeCatalogStats(stats, []string{"tv"})
	if scoped == nil {
		t.Fatal("scopeCatalogStats returned nil")
	}
	if scoped.TotalItems != 5 {
		t.Fatalf("TotalItems = %d, want 5", scoped.TotalItems)
	}
	if len(scoped.MediaTypeCounts) != 1 || scoped.MediaTypeCounts[0].MediaType != "series" {
		t.Fatalf("MediaTypeCounts = %#v, want only series", scoped.MediaTypeCounts)
	}
	if len(scoped.LibraryCounts) != 1 || scoped.LibraryCounts[0].LibraryID != "2" {
		t.Fatalf("LibraryCounts = %#v, want only TV library", scoped.LibraryCounts)
	}
	if len(scoped.QualityCounts) != 1 || scoped.QualityCounts[0].Count != 7 {
		t.Fatalf("QualityCounts = %#v, want preserved quality counts", scoped.QualityCounts)
	}
}

func TestLiveListenerChangeDetectsPortOrBindUpdates(t *testing.T) {
	cur := pluginrt.Config{PublicPort: 9999, StandaloneHTTPListen: ":9999"}
	if liveListenerChange(cur, pluginrt.Config{PublicPort: 9999, StandaloneHTTPListen: ":9999"}) {
		t.Fatal("liveListenerChange reported a change for identical listener settings")
	}
	if !liveListenerChange(cur, pluginrt.Config{PublicPort: 9998, StandaloneHTTPListen: ":9998"}) {
		t.Fatal("liveListenerChange should detect listener changes")
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

func TestCatalogRequiresPasswordWhenNoBypassTokenOrSession(t *testing.T) {
	h := New(Deps{
		Host:            func() Host { return &fakeHost{} },
		TokenSecret:     testSecret,
		CatalogPassword: "let-me-in",
	})

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mode":"catalog"`) || !strings.Contains(rec.Body.String(), `"authRequired":true`) {
		t.Fatalf("catalog should bootstrap the SPA password gate, got %s", rec.Body.String())
	}
}

func TestCatalogLoginCookieAllowsCatalogAPI(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:            func() Host { return host },
		TokenSecret:     testSecret,
		CatalogPassword: "let-me-in",
	})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/public/catalog-login", bytes.NewBufferString(`{"password":"let-me-in"}`))
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a session cookie")
	}

	mediaReq := httptest.NewRequest(http.MethodGet, "/api/catalog/media", nil)
	for _, cookie := range cookies {
		mediaReq.AddCookie(cookie)
	}
	mediaRec := httptest.NewRecorder()
	h.ServeHTTP(mediaRec, mediaReq)

	if mediaRec.Code != http.StatusOK {
		t.Fatalf("media status = %d, want 200; body=%s", mediaRec.Code, mediaRec.Body.String())
	}
}

func TestCreateTokenGeneratesPermanentBypassToken(t *testing.T) {
	h := New(Deps{
		Host:            func() Host { return &fakeHost{} },
		TokenSecret:     testSecret,
		CatalogPassword: "let-me-in",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"mediaTypes":["movie"]}`))
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Token == "" || body.URL == "" {
		t.Fatalf("token response missing token/url: %+v", body)
	}
	if strings.Contains(rec.Body.String(), "expiresAt") {
		t.Fatalf("permanent bypass token response must not include expiresAt: %s", rec.Body.String())
	}
	claims, err := verifyToken(testSecret, body.Token, time.Now().Add(10*365*24*time.Hour))
	if err != nil {
		t.Fatalf("permanent token should verify far in the future: %v", err)
	}
	if claims.ExpiresAt != 0 {
		t.Fatalf("ExpiresAt = %d, want 0 for permanent bypass token", claims.ExpiresAt)
	}
}

func TestBypassTokenAllowsPasswordProtectedCatalogAPI(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:            func() Host { return host },
		TokenSecret:     testSecret,
		CatalogPassword: "let-me-in",
	})
	token, err := signToken(testSecret, tokenClaims{
		Scope:      "catalog",
		MediaTypes: []string{"movie"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/media?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.Join(host.mediaReq.MediaTypes, ","); got != "movie" {
		t.Fatalf("media types = %q, want movie", got)
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

func TestStatsEndpointKeepsHostStatsWhenOptionalSourceFails(t *testing.T) {
	host := &fakeHost{}
	h := New(Deps{
		Host:                func() Host { return host },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		StatsCacheTTL:       time.Millisecond,
		Sources:             []CatalogSource{failingCatalogSource{}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/public/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"totalItems":12`) {
		t.Fatalf("stats should keep host totals when optional source fails, got %s", rec.Body.String())
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
	for _, want := range []string{`id="public-catalog-bootstrap"`, `"mode":"catalog"`, `"authRequired":false`, `"token":"` + token + `"`, `src="./assets/`, `href="./assets/`} {
		if !strings.Contains(body, want) {
			t.Fatalf("catalog SPA shell missing %q", want)
		}
	}
	if strings.Contains(body, `src="/assets/`) || strings.Contains(body, `href="/assets/`) {
		t.Fatalf("catalog page should use proxy-relative asset URLs")
	}
}

func TestLandingPageShowsStatsPublicHTMLAndCatalogLink(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		AdHTML:              "<p>Public offer</p>",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`id="public-catalog-bootstrap"`, `"mode":"landing"`, `Public offer`, `"catalogHref":"catalog"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing page missing %q", want)
		}
	}
}

func TestStatsHTMLFallsBackToTotalItemCard(t *testing.T) {
	body := statsHTML(&store.CatalogStats{TotalItems: 12})

	if !strings.Contains(body, `<strong>12</strong>`) {
		t.Fatalf("stats should include total count fallback, got %s", body)
	}
	if !strings.Contains(body, `Total items`) {
		t.Fatalf("stats should label total fallback, got %s", body)
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
	for _, want := range []string{"Saved access links", "Media types", "All content", "Library IDs", "HTML section editor", "Refresh stats", "Public settings", "Link name", "Custom page HTML", "Saved custom links"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}
	for _, want := range []string{"publicPort", "publicBaseURL", "catalogPassword", "ebookInstallationID", "audioInstallationID", "saveName", "customHTML", "api/admin/catalog-links"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin page missing config field %q", want)
		}
	}
	if !strings.Contains(body, "Trusted HTML") {
		t.Fatalf("admin page should label ad_html as trusted operator HTML")
	}
	if strings.Contains(body, "Expires in hours") || strings.Contains(body, "Default TTL") {
		t.Fatalf("admin page should not expose expiration controls: %s", body)
	}
	if strings.Contains(body, `fetch("/api/admin/catalog-token"`) {
		t.Fatalf("admin page should not fetch from the host root")
	}
}

func TestCreateTokenWithEmptyMediaTypesAllowsAllContent(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"mediaTypes":[]}`))
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	claims, err := verifyToken(testSecret, body.Token, time.Now())
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if len(claims.MediaTypes) != 0 {
		t.Fatalf("media types = %#v, want unrestricted all-content token", claims.MediaTypes)
	}
}

func TestAdminOpenPublicPageUsesStandaloneListener(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &fakeHost{} },
		TokenSecret:         testSecret,
		StandaloneListen:    ":9999",
		DefaultTokenTTLHour: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "https://ct.wave-ninja.eu/admin", nil)
	req.Header.Set("X-Continuum-User-Role", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `href="https://ct.wave-ninja.eu:9999/"`) {
		t.Fatalf("admin page should link public page to listener port, got %s", rec.Body.String())
	}
}

func TestAdminCanSaveAndRenderPublishedHTML(t *testing.T) {
	store := &memoryHTMLStore{}
	h := New(Deps{
		Host:            func() Host { return &fakeHost{} },
		TokenSecret:     testSecret,
		CatalogPassword: "let-me-in",
		AdHTML:          "<p>Fallback</p>",
		HTMLStore:       store,
	})

	saveReq := httptest.NewRequest(http.MethodPut, "/api/admin/html-section", bytes.NewBufferString(`{"html":"<p>Published</p>"}`))
	saveReq.Header.Set("X-Continuum-User-Role", "admin")
	saveRec := httptest.NewRecorder()
	h.ServeHTTP(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body=%s", saveRec.Code, saveRec.Body.String())
	}

	landingReq := httptest.NewRequest(http.MethodGet, "/", nil)
	landingRec := httptest.NewRecorder()
	h.ServeHTTP(landingRec, landingReq)

	if landingRec.Code != http.StatusOK {
		t.Fatalf("landing status = %d, want 200; body=%s", landingRec.Code, landingRec.Body.String())
	}
	if !strings.Contains(landingRec.Body.String(), "Published") {
		t.Fatalf("landing should render saved HTML, got %s", landingRec.Body.String())
	}
	if strings.Contains(landingRec.Body.String(), "Fallback") {
		t.Fatalf("landing should prefer saved HTML over fallback config, got %s", landingRec.Body.String())
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

func TestCreateTokenIgnoresLegacyTTLFields(t *testing.T) {
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
		Token string `json:"token"`
		URL   string `json:"url"`
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
	if strings.Contains(rec.Body.String(), "expiresAt") {
		t.Fatalf("permanent bypass token response must not include expiresAt: %s", rec.Body.String())
	}
	claims, err := verifyToken(testSecret, body.Token, start.Add(24*365*time.Hour).Add(time.Minute))
	if err != nil {
		t.Fatalf("verify permanent token: %v", err)
	}
	if claims.ExpiresAt != 0 {
		t.Fatalf("ExpiresAt = %d, want 0", claims.ExpiresAt)
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
