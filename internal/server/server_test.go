package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginrt "github.com/RXWatcher/silo-plugin-public-catalog/internal/runtime"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

const testSecret = "01234567890123456789012345678901"

// fakeHost is a minimal in-memory implementation of the Host interface
// used to wire tests without a real plugin host.
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

type panickingStatsHost struct {
	fakeHost
}

func (p *panickingStatsHost) GetCatalogStats(context.Context, []string) (*runtimehost.CatalogStats, error) {
	panic("runtime host stats client unavailable")
}

type failingCatalogSource struct{}

func (f failingCatalogSource) MediaType() string { return "ebook" }
func (f failingCatalogSource) Stats(context.Context, Host) (*runtimehost.CatalogStats, error) {
	return nil, errors.New("upstream unavailable")
}
func (f failingCatalogSource) List(context.Context, Host, runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error) {
	return nil, errors.New("upstream unavailable")
}

// fakeConfigStore is an in-memory ConfigStore for tests. Only the
// methods exercised by tests are implemented in full; the rest return
// zero values so tests that don't touch them don't have to set them up.
type fakeConfigStore struct {
	mu    sync.Mutex
	cfg   pluginrt.Config
	links []store.SavedCatalogLink
}

func newFakeConfigStore() *fakeConfigStore { return &fakeConfigStore{} }

func (s *fakeConfigStore) snapshot() pluginrt.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *fakeConfigStore) GetConfig(context.Context) (pluginrt.Config, error) {
	return s.snapshot(), nil
}

func (s *fakeConfigStore) UpdateConfig(_ context.Context, cfg pluginrt.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.CatalogPassword != "" {
		hash, err := store.HashCatalogPassword(cfg.CatalogPassword)
		if err != nil {
			return err
		}
		cfg.CatalogPasswordHash = hash
		cfg.CatalogPassword = ""
	}
	s.cfg = cfg
	return nil
}

func (s *fakeConfigStore) ClearCatalogPassword(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.CatalogPasswordHash = ""
	s.cfg.CatalogPassword = ""
	s.cfg.CatalogPasswordRequired = false
	return nil
}

func (s *fakeConfigStore) RehashCatalogPassword(_ context.Context, plaintext string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, err := store.HashCatalogPassword(plaintext)
	if err != nil {
		return err
	}
	s.cfg.CatalogPasswordHash = hash
	s.cfg.CatalogPassword = ""
	return nil
}

// SetPassword is a test-only seed helper. Hashes the plaintext like
// production code, and flips the required flag on so the catalog gate
// is active.
func (s *fakeConfigStore) SetPassword(t *testing.T, plaintext string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, err := store.HashCatalogPassword(plaintext)
	if err != nil {
		t.Fatalf("hash catalog password: %v", err)
	}
	s.cfg.CatalogPasswordHash = hash
	s.cfg.CatalogPasswordRequired = true
}

func (s *fakeConfigStore) SetPublishedHTML(html string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.PublishedHTML = html
}

func (s *fakeConfigStore) SetAdHTML(html string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.AdHTML = html
}

func (s *fakeConfigStore) CatalogStats(context.Context, []string) (*store.CatalogStats, error) {
	return &store.CatalogStats{}, nil
}
func (s *fakeConfigStore) CatalogMedia(context.Context, store.CatalogMediaQuery) (*store.CatalogMediaResponse, error) {
	return &store.CatalogMediaResponse{Items: []store.CatalogMediaItem{}}, nil
}
func (s *fakeConfigStore) CatalogFilters(context.Context, []string, []string) (*store.CatalogFilters, error) {
	return &store.CatalogFilters{}, nil
}
func (s *fakeConfigStore) CatalogItemDetail(context.Context, string, []string) (*store.CatalogItemDetail, error) {
	return nil, errors.New("not found")
}
func (s *fakeConfigStore) CatalogSeriesSeasons(context.Context, string, []string) ([]store.CatalogSeason, error) {
	return nil, nil
}
func (s *fakeConfigStore) CatalogSeasonEpisodes(context.Context, string, int, []string) ([]store.CatalogEpisode, error) {
	return nil, nil
}
func (s *fakeConfigStore) CatalogMediaImagePaths(context.Context, []string) (map[string]store.CatalogMediaImagePaths, error) {
	return nil, nil
}

func (s *fakeConfigStore) SaveCatalogLink(_ context.Context, link store.SavedCatalogLink) (store.SavedCatalogLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link.ID = len(s.links) + 1
	link.CreatedAt = time.Now()
	link.UpdatedAt = link.CreatedAt
	s.links = append(s.links, link)
	return link, nil
}

func (s *fakeConfigStore) ListCatalogLinks(context.Context) ([]store.SavedCatalogLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.SavedCatalogLink, len(s.links))
	copy(out, s.links)
	return out, nil
}

func (s *fakeConfigStore) GetCatalogLinkByName(_ context.Context, name string) (store.SavedCatalogLink, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.links {
		if strings.EqualFold(l.Name, name) {
			return l, true, nil
		}
	}
	return store.SavedCatalogLink{}, false, nil
}

func (s *fakeConfigStore) DeleteCatalogLink(_ context.Context, id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, l := range s.links {
		if l.ID == id {
			s.links = append(s.links[:i], s.links[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

// newTestDeps builds Deps with a fakeConfigStore + fakeHost. Tests can
// seed cfg or override host afterwards.
func newTestDeps() (Deps, *fakeConfigStore, *fakeHost) {
	host := &fakeHost{}
	cfg := newFakeConfigStore()
	d := Deps{
		Host:                func() Host { return host },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		ConfigStore:         cfg,
	}
	return d, cfg, host
}

// adminHeaders sets the host-stamped identity headers an admin route
// expects.
func adminHeaders(r *http.Request) {
	r.Header.Set("X-Silo-User-Id", "1")
	r.Header.Set("X-Silo-User-Role", "admin")
}

// --- Tests ---

func TestPublicStatsReturnsJSONWhenHostStatsPanics(t *testing.T) {
	h := New(Deps{
		Host:                func() Host { return &panickingStatsHost{} },
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		ConfigStore:         newFakeConfigStore(),
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

func TestCatalogMediaRejectsUnavailableMediaType(t *testing.T) {
	d, _, host := newTestDeps()
	h := New(d)
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

func TestCatalogMediaReportsSingleSourceFailure(t *testing.T) {
	d, _, _ := newTestDeps()
	d.Sources = []CatalogSource{failingCatalogSource{}}
	h := New(d)
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
	// "tv" requested → matches stored "series" rows (tv normalises to
	// series in the wire-shape stats).
	scoped := scopeCatalogStats(stats, []string{"tv"})
	if scoped == nil {
		t.Fatal("scopeCatalogStats returned nil")
	}
	if scoped.TotalItems != 5 {
		t.Fatalf("TotalItems = %d, want 5", scoped.TotalItems)
	}
}

func TestLiveListenerChangeDetectsPortOrBindUpdates(t *testing.T) {
	cur := pluginrt.Config{PublicPort: 9999, StandaloneHTTPListen: ":9999"}
	if liveListenerChange(cur, cur) {
		t.Fatal("liveListenerChange reported a change for identical listener settings")
	}
	if !liveListenerChange(cur, pluginrt.Config{PublicPort: 9998, StandaloneHTTPListen: ":9998"}) {
		t.Fatal("liveListenerChange should detect listener changes")
	}
}

func TestSecurityHeadersAndCatalogNoStore(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)
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
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)

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

func TestCatalogOpenWhenPasswordRequirementDisabled(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	cur := cfg.snapshot()
	cur.CatalogPasswordRequired = false
	cur.CatalogPasswordHash = ""
	cfg.mu.Lock()
	cfg.cfg = cur
	cfg.mu.Unlock()
	h := New(d)

	req := httptest.NewRequest(http.MethodGet, "/catalog", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when password not required; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"authRequired":false`) {
		t.Fatalf("catalog should not prompt when password requirement is disabled, got %s", rec.Body.String())
	}
}

func TestClearCatalogPasswordEndpoint(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/catalog-password", nil)
	adminHeaders(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	cur := cfg.snapshot()
	if cur.CatalogPasswordHash != "" {
		t.Fatalf("CatalogPasswordHash should be cleared, got %q", cur.CatalogPasswordHash)
	}
	if cur.CatalogPasswordRequired {
		t.Fatal("CatalogPasswordRequired should be false after clear")
	}
}

func TestCatalogLoginCookieAllowsCatalogAPI(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)

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

func TestCatalogLoginRejectsWrongPassword(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/public/catalog-login", bytes.NewBufferString(`{"password":"wrong"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCatalogLoginAcceptsLegacyPlaintextAndRehashes(t *testing.T) {
	d, cfg, _ := newTestDeps()
	// Seed legacy plaintext directly (no bcrypt hash).
	cur := cfg.snapshot()
	cur.CatalogPassword = "let-me-in"
	cur.CatalogPasswordRequired = true
	cfg.mu.Lock()
	cfg.cfg = cur
	cfg.mu.Unlock()
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/public/catalog-login", bytes.NewBufferString(`{"password":"let-me-in"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// The rehash happens in a goroutine — give it a moment.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cfg.snapshot().CatalogPasswordHash != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("legacy plaintext password should have been rehashed to bcrypt")
}

func TestCreateTokenGeneratesPermanentBypassToken(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"mediaTypes":["movie"]}`))
	adminHeaders(req)
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
	d, cfg, _ := newTestDeps()
	cfg.SetPassword(t, "let-me-in")
	h := New(d)
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
}

func TestStatsEndpointUsesShortCache(t *testing.T) {
	d, _, host := newTestDeps()
	d.StatsCacheTTL = time.Minute
	h := New(d)

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
	d, _, _ := newTestDeps()
	d.StatsCacheTTL = time.Millisecond
	d.Sources = []CatalogSource{failingCatalogSource{}}
	h := New(d)

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

func TestCatalogPageReturnsAuthorisedBootstrapWithBypassToken(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)
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
	for _, want := range []string{`"mode":"catalog"`, `"authRequired":false`, `"token":"` + token + `"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("catalog SPA shell missing %q", want)
		}
	}
}

func TestLandingPageBootstrapsAdHTMLAndCatalogLink(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetAdHTML("<p>Public offer</p>")
	h := New(d)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	for _, want := range []string{`"mode":"landing"`, `Public offer`, `"catalogHref":"catalog"`} {
		if !strings.Contains(rec.Body.String(), want) {
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

func TestAdminRouteRequiresAdminAndServesSPA(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)

	// Without identity: rejected.
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin without identity status = %d, want 401", rec.Code)
	}

	// With admin role: SPA shell baked with mode=admin so the React
	// app renders the admin console.
	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	adminHeaders(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"mode":"admin"`) {
		t.Fatalf("admin SPA should bootstrap with mode=admin, got %s", rec.Body.String())
	}
}

func TestSaveAndRenderPublishedHTML(t *testing.T) {
	d, cfg, _ := newTestDeps()
	cfg.SetAdHTML("<p>Fallback</p>")
	h := New(d)

	saveReq := httptest.NewRequest(http.MethodPut, "/api/admin/html-section", bytes.NewBufferString(`{"html":"<p>Published</p>"}`))
	adminHeaders(saveReq)
	saveRec := httptest.NewRecorder()
	h.ServeHTTP(saveRec, saveReq)
	if saveRec.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200; body=%s", saveRec.Code, saveRec.Body.String())
	}

	landingRec := httptest.NewRecorder()
	h.ServeHTTP(landingRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if landingRec.Code != http.StatusOK {
		t.Fatalf("landing status = %d, want 200; body=%s", landingRec.Code, landingRec.Body.String())
	}
	if !strings.Contains(landingRec.Body.String(), "Published") {
		t.Fatalf("landing should render saved HTML, got %s", landingRec.Body.String())
	}
	if strings.Contains(landingRec.Body.String(), "Fallback") {
		t.Fatalf("landing should prefer saved HTML over fallback ad_html, got %s", landingRec.Body.String())
	}
}

func TestNilHostStatsReturnsEmptyPayloadInsteadOfPanicking(t *testing.T) {
	h := New(Deps{
		TokenSecret:         testSecret,
		DefaultTokenTTLHour: 1,
		ConfigStore:         newFakeConfigStore(),
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
}

func TestCreateTokenWithEmptyMediaTypesAllowsAllContent(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"mediaTypes":[]}`))
	adminHeaders(req)
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

func TestCreateTokenRejectsMalformedJSON(t *testing.T) {
	d, _, _ := newTestDeps()
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString("{"))
	adminHeaders(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateTokenIgnoresLegacyTTLFields(t *testing.T) {
	d, _, _ := newTestDeps()
	d.DefaultTokenTTLHour = 0
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", bytes.NewBufferString(`{"hours":999999}`))
	adminHeaders(req)
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
	if strings.HasPrefix(body.URL, "/") {
		t.Fatalf("relative plugin catalog URL must not start at host root: %q", body.URL)
	}
	if !strings.HasPrefix(body.URL, "catalog?token=") {
		t.Fatalf("catalog URL = %q, want plugin-relative catalog link", body.URL)
	}
	if strings.Contains(rec.Body.String(), "expiresAt") {
		t.Fatalf("permanent bypass token response must not include expiresAt: %s", rec.Body.String())
	}
	claims, err := verifyToken(testSecret, body.Token, time.Now().Add(24*365*time.Hour).Add(time.Minute))
	if err != nil {
		t.Fatalf("verify permanent token: %v", err)
	}
	if claims.ExpiresAt != 0 {
		t.Fatalf("ExpiresAt = %d, want 0", claims.ExpiresAt)
	}
}

func TestCreateTokenReturnsRelativeURLWhenListenerIsWildcardAndNoPublicBaseURL(t *testing.T) {
	// `:8090` (and `0.0.0.0:8090`) bind every interface — the request
	// Host header could be the admin host, the public host, or anything
	// in between, so synthesising an absolute URL from it would silently
	// generate misleading bypass links. The handler must keep the URL
	// plugin-relative until the operator sets `public_base_url`.
	for _, addr := range []string{":8090", "0.0.0.0:8090"} {
		t.Run(addr, func(t *testing.T) {
			d, _, _ := newTestDeps()
			d.StandaloneListen = addr
			h := New(d)

			req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", http.NoBody)
			adminHeaders(req)
			req.Host = "admin.example:8443"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !strings.HasPrefix(body.URL, "catalog?token=") {
				t.Fatalf("URL = %q, want plugin-relative catalog link", body.URL)
			}
		})
	}
}

func TestCreateTokenUsesPublicBaseURLWhenSet(t *testing.T) {
	d, _, _ := newTestDeps()
	d.PublicBaseURL = "https://catalog.example.com"
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", http.NoBody)
	adminHeaders(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(body.URL, "https://catalog.example.com/catalog?token=") {
		t.Fatalf("URL = %q, want PublicBaseURL prefix", body.URL)
	}
}

func TestCreateTokenAcceptsEmptyBodyWithDefaultTTL(t *testing.T) {
	d, _, _ := newTestDeps()
	d.DefaultTokenTTLHour = 2
	h := New(d)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/catalog-token", http.NoBody)
	adminHeaders(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
