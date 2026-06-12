// Package server is the chi-mounted HTTP handler for the public-catalog
// plugin. It serves the public landing page and catalog browser to
// anonymous visitors, the admin console to Silo operators, and a
// JSON API used by both surfaces.
//
// Files in this package:
//
//	server.go           router + Deps + New
//	middleware.go       securityHeaders + requireAdmin
//	response.go         writeJSON / writeErr / writeHTML / writeInternal
//	validation.go       boundedInt, mergeMediaTypes, etc.
//	cache.go            statsCache + catalogCache
//	host.go             host SDK adapter + safeGet helpers
//	spa.go              embedded SPA serving + bootstrap rendering
//	stats.go            catalog-stats aggregation + transform helpers
//	sources.go          federated stats from companion plugin sources
//	token.go            HMAC token sign/verify
//	handlers_landing.go /, /catalog, /item, /admin SPA entry points
//	handlers_catalog.go catalog data API
//	handlers_auth.go    /api/public/catalog-login + claims extraction
//	handlers_admin_api.go admin JSON endpoints (config, links, html)
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hashicorp/go-hclog"

	pluginrt "github.com/RXWatcher/silo-plugin-public-catalog/internal/runtime"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimehost"
)

// Host is the subset of the host SDK the server depends on. Captured as
// an interface so tests can stub it without spinning up a real host.
type Host interface {
	ListLibraryMedia(ctx context.Context, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error)
	GetCatalogStats(ctx context.Context, libraryIDs []string) (*runtimehost.CatalogStats, error)
	ResolveCatalogImageURLs(ctx context.Context, paths []string, variant string) (map[string]string, error)
	CallPluginHTTP(ctx context.Context, req runtimehost.CallPluginHTTPRequest) (*runtimehost.CallPluginHTTPResponse, error)
	CallPluginJSON(ctx context.Context, req runtimehost.CallPluginJSONRequest) error
}

// ConfigStore is the subset of *store.Store the server depends on.
// Captured as an interface so tests can inject a stub without needing
// a real Postgres connection. The concrete *store.Store satisfies this
// interface implicitly.
type ConfigStore interface {
	GetConfig(ctx context.Context) (pluginrt.Config, error)
	UpdateConfig(ctx context.Context, cfg pluginrt.Config) error
	ClearCatalogPassword(ctx context.Context) error
	RehashCatalogPassword(ctx context.Context, plaintext string) error

	// ScopePublicLibraryIDs floors a requested/token library scope to the
	// operator allowlist; denyAll is true when nothing may be exposed.
	ScopePublicLibraryIDs(requested []string) (ids []string, denyAll bool)

	CatalogStats(ctx context.Context, libraryIDs []string) (*store.CatalogStats, error)
	CatalogMedia(ctx context.Context, q store.CatalogMediaQuery) (*store.CatalogMediaResponse, error)
	CatalogFilters(ctx context.Context, libraryIDs, mediaTypes []string) (*store.CatalogFilters, error)
	CatalogItemDetail(ctx context.Context, contentID string, libraryIDs []string) (*store.CatalogItemDetail, error)
	CatalogSeriesSeasons(ctx context.Context, seriesID string, libraryIDs []string) ([]store.CatalogSeason, error)
	CatalogSeasonEpisodes(ctx context.Context, seriesID string, seasonNumber int, libraryIDs []string) ([]store.CatalogEpisode, error)
	CatalogMediaImagePaths(ctx context.Context, mediaIDs []string) (map[string]store.CatalogMediaImagePaths, error)

	SaveCatalogLink(ctx context.Context, link store.SavedCatalogLink) (store.SavedCatalogLink, error)
	ListCatalogLinks(ctx context.Context) ([]store.SavedCatalogLink, error)
	GetCatalogLinkBySlug(ctx context.Context, slug string) (store.SavedCatalogLink, bool, error)
	DeleteCatalogLink(ctx context.Context, id int) error
}

// Deps wires server collaborators. Most fields are read-only after New()
// returns. ConfigStore must be non-nil — config + saved links + cached
// images all flow through it.
type Deps struct {
	Host                func() Host
	DatabaseURL         string
	Logger              hclog.Logger
	TokenSecret         string
	PublicBaseURL       string
	StandaloneListen    string
	DefaultTokenTTLHour int
	StatsCacheTTL       time.Duration
	Sources             []CatalogSource
	ConfigStore         ConfigStore
	ApplyConfig         func(pluginrt.Config) error

	statsCache   *statsCache
	catalogCache *catalogCache

	loginLimiter  *loginLimiter
	publicLimiter *ipRateLimiter
	bcryptSem     *bcryptSemaphore
}

// New constructs the fully-composed handler. Routes:
//
//   - /                                public landing page (SPA)
//   - /catalog                         catalog browser (SPA, password-gated)
//   - /item/{id}                       item detail (SPA)
//   - /admin                           admin console (SPA, admin-gated)
//   - /assets/*                        embedded SPA assets
//   - /api/public/stats                anonymous stats
//   - /api/public/catalog-login        password → session cookie
//   - /api/catalog/*                   gated catalog JSON API
//   - /api/admin/*                     admin JSON API
func New(d Deps) http.Handler {
	if d.StatsCacheTTL <= 0 {
		d.StatsCacheTTL = 30 * time.Second
	}
	d.statsCache = &statsCache{ttl: d.StatsCacheTTL}
	d.catalogCache = newCatalogCache(2 * time.Minute)

	// Abuse controls. Login gets a strict per-IP attempt limiter plus a
	// lockout; all bcrypt verifications share a global concurrency cap so
	// a burst can't pin every CPU. The other public read endpoints get a
	// looser per-IP request limiter.
	d.loginLimiter = newLoginLimiter(8, 5*time.Minute)
	d.publicLimiter = newIPRateLimiter(120, time.Minute)
	d.bcryptSem = newBcryptSemaphore(maxConcurrentBcrypt())

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/", hLanding(d))
	r.Get("/catalog", hCatalogPage(d))
	r.Get("/item/{id}", hItemPage(d))
	r.Get("/assets/*", hPublicAsset())
	r.Get("/admin", requireAdmin(hAdminPage(d)))

	r.Get("/api/public/stats", rateLimitMiddleware(d.publicLimiter, hStats(d)))
	r.Post("/api/public/catalog-login", hCatalogLogin(d))

	r.Get("/api/catalog/media", rateLimitMiddleware(d.publicLimiter, hCatalogMedia(d)))
	r.Get("/api/catalog/filters", rateLimitMiddleware(d.publicLimiter, hCatalogFilters(d)))
	r.Get("/api/catalog/items/{id}", rateLimitMiddleware(d.publicLimiter, hCatalogItemDetail(d)))
	r.Get("/api/catalog/items/{id}/seasons", rateLimitMiddleware(d.publicLimiter, hCatalogSeriesSeasons(d)))
	r.Get("/api/catalog/series/{id}/seasons/{season}/episodes", rateLimitMiddleware(d.publicLimiter, hCatalogSeasonEpisodes(d)))

	r.Post("/api/admin/catalog-token", requireAdmin(hCreateToken(d)))
	r.Get("/api/admin/catalog-links", requireAdmin(hListCatalogLinks(d)))
	r.Delete("/api/admin/catalog-links/{id}", requireAdmin(hDeleteCatalogLink(d)))
	r.Get("/api/admin/html-section", requireAdmin(hGetHTMLSection(d)))
	r.Put("/api/admin/html-section", requireAdmin(hSaveHTMLSection(d)))
	r.Get("/api/admin/config", requireAdmin(hGetConfig(d)))
	r.Patch("/api/admin/config", requireAdmin(hUpdateConfig(d)))
	r.Delete("/api/admin/catalog-password", requireAdmin(hClearCatalogPassword(d)))
	return r
}
