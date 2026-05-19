package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hashicorp/go-hclog"

	pluginrt "github.com/ContinuumApp/continuum-plugin-public-catalog/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/store"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

//go:embed public/dist/* public/dist/assets/*
var publicSPA embed.FS

type Host interface {
	ListLibraryMedia(ctx context.Context, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error)
	GetCatalogStats(ctx context.Context, libraryIDs []string) (*runtimehost.CatalogStats, error)
	ResolveCatalogImageURLs(ctx context.Context, paths []string, variant string) (map[string]string, error)
	CallPluginHTTP(ctx context.Context, req runtimehost.CallPluginHTTPRequest) (*runtimehost.CallPluginHTTPResponse, error)
	CallPluginJSON(ctx context.Context, req runtimehost.CallPluginJSONRequest) error
}

type Deps struct {
	Host                 func() Host
	DatabaseURL          string
	Logger               hclog.Logger
	TokenSecret          string
	TokenSecretGenerated bool
	PublicBaseURL        string
	AdHTML               string
	CatalogPassword      string
	StandaloneListen     string
	DefaultTokenTTLHour  int
	StatsCacheTTL        time.Duration
	Sources              []CatalogSource
	HTMLStore            HTMLStore
	ConfigStore          *store.Store
	ApplyConfig          func(pluginrt.Config) error

	statsCache   *statsCache
	catalogCache *catalogCache
}

func New(d Deps) http.Handler {
	if d.StatsCacheTTL <= 0 {
		d.StatsCacheTTL = 30 * time.Second
	}
	d.statsCache = &statsCache{ttl: d.StatsCacheTTL}
	d.catalogCache = newCatalogCache(2 * time.Minute)
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/", hLanding(d))
	r.Get("/catalog", hCatalogPage(d))
	r.Get("/item/{id}", hItemPage(d))
	r.Get("/assets/*", hPublicAsset())
	r.Get("/api/public/stats", hStats(d))
	r.Post("/api/public/catalog-login", hCatalogLogin(d))
	r.Get("/api/catalog/media", hCatalogMedia(d))
	r.Get("/api/catalog/filters", hCatalogFilters(d))
	r.Get("/api/catalog/items/{id}", hCatalogItemDetail(d))
	r.Get("/api/catalog/items/{id}/seasons", hCatalogSeriesSeasons(d))
	r.Get("/api/catalog/series/{id}/seasons/{season}/episodes", hCatalogSeasonEpisodes(d))
	r.Post("/api/admin/catalog-token", requireAdmin(hCreateToken(d)))
	r.Get("/api/admin/catalog-links", requireAdmin(hListCatalogLinks(d)))
	r.Delete("/api/admin/catalog-links/{id}", requireAdmin(hDeleteCatalogLink(d)))
	r.Get("/api/admin/html-section", requireAdmin(hGetHTMLSection(d)))
	r.Put("/api/admin/html-section", requireAdmin(hSaveHTMLSection(d)))
	r.Get("/api/admin/config", requireAdmin(hGetConfig(d)))
	r.Patch("/api/admin/config", requireAdmin(hUpdateConfig(d)))
	r.Get("/admin", requireAdmin(hAdminPage(d)))
	return r
}

func hGetConfig(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		cfg, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "config_failed", err.Error())
			return
		}
		cfg.TokenSecret = ""
		cfg.CatalogPassword = ""
		writeJSON(w, http.StatusOK, cfg)
	}
}

func hUpdateConfig(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		cur, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "config_failed", err.Error())
			return
		}
		var req pluginrt.Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}
		if req.TokenSecret == "" {
			req.TokenSecret = cur.TokenSecret
		}
		if req.CatalogPassword == "" {
			req.CatalogPassword = cur.CatalogPassword
		}
		next, err := pluginrt.NormalizeAppConfig(req)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "config_failed", err.Error())
			return
		}
		if liveListenerChange(cur, next) {
			writeErr(w, http.StatusBadRequest, "config_failed", "changing the public listener requires a plugin restart; keep the current listener settings or restart after updating them")
			return
		}
		if err := d.ConfigStore.UpdateConfig(r.Context(), next); err != nil {
			writeErr(w, http.StatusBadRequest, "config_failed", err.Error())
			return
		}
		cfg, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "config_failed", err.Error())
			return
		}
		if d.ApplyConfig != nil {
			cfg.DatabaseURL = d.DatabaseURL
			if err := d.ApplyConfig(cfg); err != nil {
				writeErr(w, http.StatusBadRequest, "config_failed", err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "base-uri 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/catalog") || strings.HasPrefix(r.URL.Path, "/api/catalog/") {
			h.Set("Cache-Control", "no-store")
		} else if strings.HasPrefix(r.URL.Path, "/api/public/") {
			h.Set("Cache-Control", "public, max-age=30")
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("X-Continuum-User-Role"), "admin") {
			next(w, r)
			return
		}
		roles := strings.Split(r.Header.Get("X-Continuum-User-Roles"), ",")
		for _, role := range roles {
			if strings.EqualFold(strings.TrimSpace(role), "admin") {
				next(w, r)
				return
			}
		}
		writeErr(w, http.StatusUnauthorized, "admin_required", "admin identity required")
	}
}

func hLanding(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var customHTML string
		catalogHref := "catalog"
		if name := strings.TrimSpace(r.URL.Query().Get("page")); name != "" && d.ConfigStore != nil {
			if link, ok, err := d.ConfigStore.GetCatalogLinkByName(r.Context(), name); err == nil && ok {
				customHTML = link.HTML
				if strings.TrimSpace(link.URL) != "" {
					catalogHref = link.URL
				}
			}
		}
		libraryIDs, mediaTypes := []string(nil), []string(nil)
		if claims, ok := catalogClaimsFromRequest(d, r); ok {
			libraryIDs = claims.LibraryIDs
			mediaTypes = claims.MediaTypes
		}
		stats, _ := publicCatalogStats(r.Context(), d, libraryIDs)
		stats = scopeCatalogStats(stats, mediaTypes)
		writePublicApp(w, r, publicBootstrap{
			Mode:         "landing",
			Theme:        adminTheme(r),
			CatalogHref:  catalogHref,
			CustomHTML:   adHTML(defaultString(customHTML, publishedHTML(r.Context(), d))),
			InitialStats: stats,
		}, http.StatusOK)
	}
}

func hCatalogPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		status := http.StatusOK
		if !ok {
			status = http.StatusUnauthorized
		}
		stats, _ := publicCatalogStats(r.Context(), d, claims.LibraryIDs)
		stats = scopeCatalogStats(stats, claims.MediaTypes)
		bootstrap := publicBootstrap{
			Mode:         "catalog",
			Theme:        adminTheme(r),
			AuthRequired: !ok,
			Token:        strings.TrimSpace(r.URL.Query().Get("token")),
			InitialStats: stats,
		}
		if ok && d.ConfigStore != nil && stats != nil {
			if libraryID := initialCatalogLibraryID(stats, claims.LibraryIDs, r.URL.Query().Get("libraryId")); libraryID != "" {
				bootstrap.InitialLibraryID = libraryID
				storeMediaTypes := normalizeStoreMediaTypes(claims.MediaTypes)
				mediaKey := catalogMediaCacheKey([]string{libraryID}, claims.MediaTypes, "", "", 0, 0, "added_at", true, 60, "")
				if resp, hit := d.catalogCache.getMedia(mediaKey); hit {
					bootstrap.InitialItems = resp.Items
					bootstrap.InitialNextPageToken = resp.NextPageToken
					bootstrap.InitialTotalCount = resp.TotalCount
				} else if resp, err := d.ConfigStore.CatalogMedia(r.Context(), store.CatalogMediaQuery{
					LibraryIDs: []string{libraryID},
					MediaTypes: storeMediaTypes,
					Sort:       "added_at",
					Descending: true,
					PageSize:   60,
				}); err == nil && resp != nil {
					overlayCatalogMediaImages(r.Context(), d, runtimehost.ListLibraryMediaRequest{
						LibraryIDs: []string{libraryID},
						MediaTypes: claims.MediaTypes,
						Sort:       "added_at",
						Descending: true,
						PageSize:   60,
					}, resp.Items)
					d.catalogCache.setMedia(mediaKey, resp)
					bootstrap.InitialItems = resp.Items
					bootstrap.InitialNextPageToken = resp.NextPageToken
					bootstrap.InitialTotalCount = resp.TotalCount
				}
				filterKey := catalogFiltersCacheKey([]string{libraryID}, claims.MediaTypes)
				if filters, hit := d.catalogCache.getFilters(filterKey); hit {
					bootstrap.InitialFilters = filters
				} else if filters, err := d.ConfigStore.CatalogFilters(r.Context(), []string{libraryID}, storeMediaTypes); err == nil && filters != nil {
					d.catalogCache.setFilters(filterKey, filters)
					bootstrap.InitialFilters = filters
				}
			}
		}
		writePublicApp(w, r, bootstrap, status)
	}
}

func hItemPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := catalogClaimsFromRequest(d, r)
		status := http.StatusOK
		if !ok {
			status = http.StatusUnauthorized
		}
		stats, _ := publicCatalogStats(r.Context(), d, nil)
		writePublicApp(w, r, publicBootstrap{
			Mode:         "detail",
			Theme:        adminTheme(r),
			AuthRequired: !ok,
			Token:        strings.TrimSpace(r.URL.Query().Get("token")),
			InitialStats: stats,
		}, status)
	}
}

type publicBootstrap struct {
	Mode                 string                   `json:"mode"`
	Theme                string                   `json:"theme"`
	CatalogHref          string                   `json:"catalogHref"`
	CustomHTML           string                   `json:"customHTML"`
	AuthRequired         bool                     `json:"authRequired"`
	Token                string                   `json:"token"`
	InitialStats         *store.CatalogStats      `json:"initialStats,omitempty"`
	InitialLibraryID     string                   `json:"initialLibraryId,omitempty"`
	InitialItems         []store.CatalogMediaItem `json:"initialItems,omitempty"`
	InitialNextPageToken string                   `json:"initialNextPageToken,omitempty"`
	InitialTotalCount    int                      `json:"initialTotalCount,omitempty"`
	InitialFilters       *store.CatalogFilters    `json:"initialFilters,omitempty"`
}

func publicCatalogPreview(ctx context.Context, d Deps, r *http.Request, limit int) []runtimehost.CatalogMediaItem {
	claims, ok := catalogClaimsFromRequest(d, r)
	if !ok {
		return nil
	}
	host := currentHost(d)
	if host == nil {
		return nil
	}
	req := runtimehost.ListLibraryMediaRequest{
		LibraryIDs: claims.LibraryIDs,
		MediaTypes: claims.MediaTypes,
		Sort:       "added_at",
		Descending: true,
		PageSize:   limit,
	}
	resp, err := host.ListLibraryMedia(ctx, req)
	if err != nil || resp == nil {
		return nil
	}
	return resp.Items
}

func liveListenerChange(cur, next pluginrt.Config) bool {
	return strings.TrimSpace(cur.StandaloneHTTPListen) != strings.TrimSpace(next.StandaloneHTTPListen) ||
		cur.PublicPort != next.PublicPort
}

func initialCatalogLibraryID(stats *store.CatalogStats, allowed []string, requested string) string {
	allowed = cleanList(allowed)
	allowedSet := map[string]bool{}
	for _, id := range allowed {
		allowedSet[id] = true
	}
	isAllowed := func(id string) bool {
		return id != "" && (len(allowedSet) == 0 || allowedSet[id])
	}
	requested = strings.TrimSpace(requested)
	if isAllowed(requested) {
		for _, library := range stats.LibraryCounts {
			if library.LibraryID == requested {
				return requested
			}
		}
	}
	for _, library := range stats.LibraryCounts {
		if isAllowed(library.LibraryID) {
			return library.LibraryID
		}
	}
	return ""
}

func scopeCatalogStats(stats *store.CatalogStats, mediaTypes []string) *store.CatalogStats {
	normalized := normalizeStoreMediaTypes(mediaTypes)
	if stats == nil || len(normalized) == 0 {
		return stats
	}
	allowed := map[string]bool{}
	for _, mediaType := range normalized {
		allowed[mediaType] = true
	}
	out := &store.CatalogStats{
		MediaTypeCounts: make([]store.CatalogTypeCount, 0, len(stats.MediaTypeCounts)),
		LibraryCounts:   make([]store.CatalogLibraryCount, 0, len(stats.LibraryCounts)),
		QualityCounts:   append([]store.CatalogQualityCount(nil), stats.QualityCounts...),
	}
	for _, count := range stats.MediaTypeCounts {
		if allowed[count.MediaType] {
			out.MediaTypeCounts = append(out.MediaTypeCounts, count)
			out.TotalItems += count.Count
		}
	}
	for _, count := range stats.LibraryCounts {
		libraryType := count.MediaType
		if libraryType == "tv" {
			libraryType = "series"
		}
		if allowed[libraryType] {
			out.LibraryCounts = append(out.LibraryCounts, count)
		}
	}
	return out
}

func hPublicAsset() http.HandlerFunc {
	dist, err := fs.Sub(publicSPA, "public/dist")
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}
	}
	handler := http.StripPrefix("/", http.FileServer(http.FS(dist)))
	return func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}
}

func writePublicApp(w http.ResponseWriter, r *http.Request, bootstrap publicBootstrap, status int) {
	if bootstrap.Theme == "" || bootstrap.Theme == "default" {
		bootstrap.Theme = "midnight-cinema"
	}
	if bootstrap.CatalogHref == "" {
		bootstrap.CatalogHref = "catalog"
	}
	index, err := publicSPA.ReadFile("public/dist/index.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "spa_unavailable", "public catalog app has not been built")
		return
	}
	rawBootstrap, err := json.Marshal(bootstrap)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bootstrap_failed", err.Error())
		return
	}
	index = bytes.Replace(index, []byte("%PUBLIC_CATALOG_BOOTSTRAP%"), rawBootstrap, 1)
	index = bytes.Replace(index, []byte(`<html lang="en">`), []byte(`<html lang="en" data-theme="`+html.EscapeString(bootstrap.Theme)+`">`), 1)
	assetBase := "./assets/"
	if strings.HasPrefix(r.URL.Path, "/item/") {
		assetBase = "../assets/"
	}
	index = bytes.ReplaceAll(index, []byte(`./assets/`), []byte(assetBase))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(index)
	_ = r
}

func hCatalogLogin(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}
		if d.CatalogPassword == "" || req.Password != d.CatalogPassword {
			writeErr(w, http.StatusUnauthorized, "invalid_password", "catalog password is invalid")
			return
		}
		token, err := signToken(d.TokenSecret, tokenClaims{Scope: "catalog"})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "token_failed", err.Error())
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "public_catalog_auth",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil,
		})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func hStats(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := publicCatalogStats(r.Context(), d, nil)
		if err != nil {
			writeJSON(w, http.StatusOK, store.CatalogStats{})
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func hCatalogMedia(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "catalog_auth_required", "catalog password or bypass token is required")
			return
		}
		q := r.URL.Query()
		mediaTypes, ok := mergeMediaTypes(claims.MediaTypes, q["media_type"])
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_media_type", "requested media type is not available for this catalog link")
			return
		}
		libraryIDs, ok := mergeLibraryIDs(claims.LibraryIDs, q["library_id"])
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_library", "requested library is not available for this catalog link")
			return
		}
		req := runtimehost.ListLibraryMediaRequest{
			LibraryIDs: libraryIDs,
			MediaTypes: mediaTypes,
			Query:      strings.TrimSpace(q.Get("q")),
			Genre:      strings.TrimSpace(q.Get("genre")),
			Sort:       defaultString(q.Get("sort"), "title"),
			Descending: q.Get("desc") == "true",
			PageSize:   boundedInt(q.Get("page_size"), 48, 1, 100),
			PageToken:  q.Get("page_token"),
		}
		req.YearMin = boundedInt(q.Get("year_min"), 0, 0, 9999)
		req.YearMax = boundedInt(q.Get("year_max"), 0, 0, 9999)
		if d.ConfigStore != nil && directCatalogMediaTypes(req.MediaTypes) {
			cacheKey := catalogMediaCacheKey(libraryIDs, req.MediaTypes, req.Query, req.Genre, req.YearMin, req.YearMax, req.Sort, req.Descending, req.PageSize, req.PageToken)
			if resp, hit := d.catalogCache.getMedia(cacheKey); hit {
				w.Header().Set("X-Public-Catalog-Cache", "HIT")
				writeJSON(w, http.StatusOK, resp)
				return
			}
			resp, err := d.ConfigStore.CatalogMedia(r.Context(), store.CatalogMediaQuery{
				LibraryIDs: libraryIDs,
				MediaTypes: normalizeStoreMediaTypes(req.MediaTypes),
				Query:      req.Query,
				Genre:      req.Genre,
				YearMin:    req.YearMin,
				YearMax:    req.YearMax,
				Sort:       req.Sort,
				Descending: req.Descending,
				PageSize:   req.PageSize,
				PageToken:  req.PageToken,
			})
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "catalog_failed", err.Error())
				return
			}
			overlayCatalogMediaImages(r.Context(), d, req, resp.Items)
			d.catalogCache.setMedia(cacheKey, resp)
			w.Header().Set("X-Public-Catalog-Cache", "MISS")
			writeJSON(w, http.StatusOK, resp)
			return
		}
		host := currentHost(d)
		if host == nil {
			writeJSON(w, http.StatusOK, emptyCatalogMediaResponse())
			return
		}
		if len(req.MediaTypes) == 1 {
			if src := sourceForType(d.Sources, req.MediaTypes[0]); src != nil {
				resp, err := src.List(r.Context(), host, req)
				if err != nil {
					writeErr(w, http.StatusBadGateway, "catalog_source_unavailable", "catalog source is unavailable")
					return
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}
		resp, err := host.ListLibraryMedia(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusOK, emptyCatalogMediaResponse())
			return
		}
		if req.PageToken == "" {
			resp = appendSourceCatalogs(r.Context(), host, resp, d.Sources, req)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func hCatalogFilters(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "catalog_auth_required", "catalog password or bypass token is required")
			return
		}
		if d.ConfigStore == nil {
			writeJSON(w, http.StatusOK, store.CatalogFilters{})
			return
		}
		q := r.URL.Query()
		mediaTypes, ok := mergeMediaTypes(claims.MediaTypes, q["media_type"])
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_media_type", "requested media type is not available for this catalog link")
			return
		}
		libraryIDs, ok := mergeLibraryIDs(claims.LibraryIDs, q["library_id"])
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_library", "requested library is not available for this catalog link")
			return
		}
		cacheKey := catalogFiltersCacheKey(libraryIDs, mediaTypes)
		if filters, hit := d.catalogCache.getFilters(cacheKey); hit {
			w.Header().Set("X-Public-Catalog-Cache", "HIT")
			writeJSON(w, http.StatusOK, filters)
			return
		}
		filters, err := d.ConfigStore.CatalogFilters(r.Context(), libraryIDs, normalizeStoreMediaTypes(mediaTypes))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "filters_failed", err.Error())
			return
		}
		d.catalogCache.setFilters(cacheKey, filters)
		w.Header().Set("X-Public-Catalog-Cache", "MISS")
		writeJSON(w, http.StatusOK, filters)
	}
}

func directCatalogMediaTypes(mediaTypes []string) bool {
	for _, mediaType := range mediaTypes {
		switch mediaType {
		case "", "movie", "series", "tv", "episode":
		default:
			return false
		}
	}
	return true
}

func normalizeStoreMediaTypes(mediaTypes []string) []string {
	out := []string{}
	for _, mediaType := range mediaTypes {
		if mediaType == "tv" {
			mediaType = "series"
		}
		if mediaType != "" {
			out = append(out, mediaType)
		}
	}
	return out
}

func emptyCatalogMediaResponse() runtimehost.ListLibraryMediaResponse {
	return runtimehost.ListLibraryMediaResponse{
		Items:      []runtimehost.CatalogMediaItem{},
		TotalCount: 0,
	}
}

func hCatalogItemDetail(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "catalog_auth_required", "catalog password or bypass token is required")
			return
		}
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "catalog_unavailable", "catalog store is not configured")
			return
		}
		id := strings.TrimSpace(chi.URLParam(r, "id"))
		item, err := d.ConfigStore.CatalogItemDetail(r.Context(), id, claims.LibraryIDs)
		if err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "catalog item was not found")
			return
		}
		enrichCatalogItemImages(r.Context(), d, claims, item)
		writeJSON(w, http.StatusOK, item)
	}
}

func hCatalogSeriesSeasons(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "catalog_auth_required", "catalog password or bypass token is required")
			return
		}
		if d.ConfigStore == nil {
			writeJSON(w, http.StatusOK, []store.CatalogSeason{})
			return
		}
		seasons, err := d.ConfigStore.CatalogSeriesSeasons(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")), claims.LibraryIDs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "seasons_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, seasons)
	}
}

func hCatalogSeasonEpisodes(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := catalogClaimsFromRequest(d, r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "catalog_auth_required", "catalog password or bypass token is required")
			return
		}
		if d.ConfigStore == nil {
			writeJSON(w, http.StatusOK, []store.CatalogEpisode{})
			return
		}
		seasonNumber, err := strconv.Atoi(chi.URLParam(r, "season"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_season", "season number is invalid")
			return
		}
		episodes, err := d.ConfigStore.CatalogSeasonEpisodes(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")), seasonNumber, claims.LibraryIDs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "episodes_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, episodes)
	}
}

func enrichCatalogItemImages(ctx context.Context, d Deps, claims tokenClaims, item *store.CatalogItemDetail) {
	host := currentHost(d)
	if host == nil || item == nil {
		return
	}
	mediaTypes := []string{item.Type}
	query := item.Title
	matchID := item.ContentID
	if item.Type == "season" || item.Type == "episode" {
		mediaTypes = []string{"tv"}
		query = item.SeriesTitle
		matchID = item.SeriesID
	}
	resp, err := host.ListLibraryMedia(ctx, runtimehost.ListLibraryMediaRequest{
		LibraryIDs: claims.LibraryIDs,
		MediaTypes: mediaTypes,
		Query:      query,
		Sort:       "title",
		PageSize:   20,
	})
	if err != nil || resp == nil {
		return
	}
	for _, candidate := range resp.Items {
		if candidate.MediaID != matchID {
			continue
		}
		if item.Type == "season" || item.Type == "episode" {
			if item.PosterURL == "" && candidate.PosterURL != "" {
				item.PosterURL = candidate.PosterURL
			}
			if candidate.BackdropURL != "" {
				item.BackdropURL = candidate.BackdropURL
			}
		} else {
			if candidate.PosterURL != "" {
				item.PosterURL = candidate.PosterURL
			}
			if candidate.BackdropURL != "" {
				item.BackdropURL = candidate.BackdropURL
			}
		}
		return
	}
}

func overlayCatalogMediaImages(ctx context.Context, d Deps, req runtimehost.ListLibraryMediaRequest, items []store.CatalogMediaItem) {
	if len(items) == 0 {
		return
	}
	if d.ConfigStore == nil {
		return
	}
	host := currentHost(d)
	if host == nil {
		return
	}
	missing := make([]string, 0, len(items))
	for _, item := range items {
		if item.MediaID == "" {
			continue
		}
		if item.PosterURL == "" || item.BackdropURL == "" {
			missing = append(missing, item.MediaID)
		}
	}
	if len(missing) == 0 {
		return
	}
	rawPaths, err := d.ConfigStore.CatalogMediaImagePaths(ctx, missing)
	if err != nil || len(rawPaths) == 0 {
		return
	}
	paths := make([]string, 0, len(rawPaths)*2)
	for _, item := range items {
		if item.MediaID == "" {
			continue
		}
		raw, ok := rawPaths[item.MediaID]
		if !ok {
			continue
		}
		if item.PosterURL == "" && strings.TrimSpace(raw.PosterPath) != "" {
			paths = append(paths, raw.PosterPath)
		}
		if item.BackdropURL == "" && strings.TrimSpace(raw.BackdropPath) != "" {
			paths = append(paths, raw.BackdropPath)
		}
	}
	if len(paths) == 0 {
		return
	}
	resolved, err := host.ResolveCatalogImageURLs(ctx, paths, "card")
	if err != nil || len(resolved) == 0 {
		return
	}
	for i := range items {
		raw, ok := rawPaths[items[i].MediaID]
		if !ok {
			continue
		}
		if items[i].PosterURL == "" {
			if value := strings.TrimSpace(resolved[raw.PosterPath]); value != "" {
				items[i].PosterURL = value
			}
		}
		if items[i].BackdropURL == "" {
			if value := strings.TrimSpace(resolved[raw.BackdropPath]); value != "" {
				items[i].BackdropURL = value
			}
		}
	}
}

type createTokenRequest struct {
	Hours      int      `json:"hours"`
	LibraryIDs []string `json:"libraryIds"`
	MediaTypes []string `json:"mediaTypes"`
	SaveName   string   `json:"saveName"`
	HTML       string   `json:"html"`
}

func hCreateToken(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTokenRequest
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				if errors.Is(err, io.EOF) {
					req = createTokenRequest{}
				} else {
					writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
					return
				}
			}
		}
		token, err := signToken(d.TokenSecret, tokenClaims{
			Scope:      "catalog",
			LibraryIDs: cleanList(req.LibraryIDs),
			MediaTypes: cleanList(req.MediaTypes),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "token_failed", err.Error())
			return
		}
		path := "catalog?token=" + url.QueryEscape(token)
		link := path
		if base := publicBaseURLForRequest(r, d); base != "" {
			link = strings.TrimRight(base, "/") + "/" + path
		}
		resp := map[string]any{
			"token": token,
			"url":   link,
		}
		if name := strings.TrimSpace(req.SaveName); name != "" {
			if d.ConfigStore == nil {
				writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
				return
			}
			saved, err := d.ConfigStore.SaveCatalogLink(r.Context(), store.SavedCatalogLink{
				Name:       name,
				Token:      token,
				URL:        link,
				HTML:       req.HTML,
				MediaTypes: cleanList(req.MediaTypes),
				LibraryIDs: cleanList(req.LibraryIDs),
			})
			if err != nil {
				writeErr(w, http.StatusBadRequest, "save_link_failed", err.Error())
				return
			}
			resp["savedLink"] = saved
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func hDeleteCatalogLink(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			writeErr(w, http.StatusBadRequest, "bad_id", "catalog link id is invalid")
			return
		}
		if err := d.ConfigStore.DeleteCatalogLink(r.Context(), id); err != nil {
			writeErr(w, http.StatusNotFound, "not_found", "catalog link was not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func hListCatalogLinks(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeJSON(w, http.StatusOK, []store.SavedCatalogLink{})
			return
		}
		links, err := d.ConfigStore.ListCatalogLinks(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "list_links_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, links)
	}
}

func hGetHTMLSection(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"html": publishedHTML(r.Context(), d)})
	}
}

func hSaveHTMLSection(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.HTMLStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "html_store_unavailable", "HTML editor storage is not configured")
			return
		}
		var req struct {
			HTML string `json:"html"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}
		if len(req.HTML) > 200_000 {
			writeErr(w, http.StatusBadRequest, "html_too_large", "HTML section must be 200 KB or smaller")
			return
		}
		if err := d.HTMLStore.SaveHTML(r.Context(), req.HTML); err != nil {
			writeErr(w, http.StatusInternalServerError, "html_save_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func catalogClaimsFromRequest(d Deps, r *http.Request) (tokenClaims, bool) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		claims, err := verifyToken(d.TokenSecret, token, time.Now())
		return claims, err == nil
	}
	if cookie, err := r.Cookie("public_catalog_auth"); err == nil && cookie.Value != "" {
		claims, err := verifyToken(d.TokenSecret, cookie.Value, time.Now())
		return claims, err == nil
	}
	if d.CatalogPassword == "" {
		return tokenClaims{Scope: "catalog"}, true
	}
	return tokenClaims{}, false
}

func writeCatalogPasswordPage(w http.ResponseWriter, r *http.Request) {
	body := `<!doctype html>
<html lang="en" data-theme="` + adminTheme(r) + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Catalog Password</title>
<style>` + css() + adminCSS() + `</style>
</head>
<body>
<main class="shell">
  <section class="hero">
    <div>
      <p class="eyebrow">Public catalog</p>
      <h1>Catalog password</h1>
      <p class="lead">Enter the catalog password to browse available media.</p>
    </div>
  </section>
  <section class="panel">
    <form id="login" class="admin-form">
      <label>Password
        <input id="password" name="password" type="password" autofocus>
      </label>
      <button class="button primary" type="submit">Open catalog</button>
    </form>
    <pre id="error" class="error-box" hidden></pre>
  </section>
</main>
<script>
login.addEventListener("submit",async event=>{
  event.preventDefault(); error.hidden=true;
  try{
    const r=await fetch("api/public/catalog-login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({password:password.value})});
    const data=await r.json().catch(()=>({}));
    if(!r.ok) throw new Error(data?.error?.message||"Password rejected");
    location.reload();
  }catch(err){error.hidden=false; error.textContent=err.message||String(err)}
});
</script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(body))
}

func publishedHTML(ctx context.Context, d Deps) string {
	if d.HTMLStore == nil {
		return d.AdHTML
	}
	html, err := d.HTMLStore.LoadHTML(ctx)
	if err != nil || strings.TrimSpace(html) == "" {
		return d.AdHTML
	}
	return html
}

func hAdminPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secretStatus := "Auto-generated"
		if strings.TrimSpace(d.TokenSecret) != "" && !d.TokenSecretGenerated {
			secretStatus = "Configured"
		}
		baseURL := d.PublicBaseURL
		if baseURL == "" {
			baseURL = "Relative plugin URLs"
		}
		listenAddress := d.StandaloneListen
		if listenAddress == "" {
			listenAddress = "Not configured"
		}
		passwordStatus := "Configured"
		if d.CatalogPassword == "" {
			passwordStatus = "Not configured"
		}
		publicPageURL := publicBaseURLForRequest(r, d)
		if publicPageURL == "" {
			publicPageURL = "../"
		}
		writeHTML(w, `<!doctype html>
<html lang="en" data-theme="`+adminTheme(r)+`">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Public Catalog Admin</title>
<style>`+css()+adminCSS()+`</style>
</head>
<body>
<main class="shell admin-shell">
  <section class="hero admin-hero">
    <div>
      <a class="button" href="/admin/plugins">&larr; Back to plugins</a>
      <p class="eyebrow">Plugin administration</p>
      <h1>Public Catalog</h1>
      <p class="lead">Publish a public landing page, protect the catalog with a password, and create permanent bypass links.</p>
    </div>
    <a class="button" id="openPublicPage" href="`+html.EscapeString(publicPageURL)+`" target="_blank" rel="noreferrer">Open public page</a>
  </section>

  <section class="admin-grid">
    <div class="panel admin-panel">
      <div class="panel-head">
        <h2>Generate bypass token</h2>
        <span id="state" class="status-pill">Ready</span>
      </div>
      <form id="tokenForm" class="admin-form">
        <fieldset class="token-scope">
          <legend>Catalog scope</legend>
          <label class="check"><input type="radio" name="tokenScope" value="all" checked> All content</label>
          <label class="check"><input type="radio" name="tokenScope" value="custom"> Selected media types</label>
        </fieldset>
        <fieldset>
          <legend>Media types</legend>
          <label class="check"><input type="checkbox" name="mediaTypes" value="movie"> Movies</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="tv"> TV</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="episode"> Episodes</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="ebook"> Ebooks</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="audiobook"> Audiobooks</label>
        </fieldset>
        <label>Library IDs
          <input id="libraryIds" name="libraryIds" placeholder="Optional comma-separated IDs">
        </label>
        <div class="settings-row compact-row">
          <label class="medium-field">Custom link name
            <input id="saveName" name="saveName" placeholder="Bree">
          </label>
        </div>
        <label>Custom page HTML
          <textarea id="customHTML" name="customHTML" rows="8" spellcheck="false" placeholder="<h2>Bree's catalog</h2>"></textarea>
        </label>
        <button class="button primary" type="submit">Save custom link</button>
      </form>
      <div id="result" class="result-box" hidden>
        <label>Catalog URL
          <textarea id="urlOut" readonly rows="3"></textarea>
        </label>
        <label>Public page URL
          <textarea id="pageUrlOut" readonly rows="2"></textarea>
        </label>
        <div class="actions">
          <button class="button" id="copy" type="button">Copy</button>
          <button class="button" id="copyPage" type="button">Copy page</button>
          <a class="button" id="openLink" href="#" target="_blank" rel="noreferrer">Open</a>
          <a class="button" id="openPageLink" href="#" target="_blank" rel="noreferrer">Open page</a>
        </div>
      </div>
      <pre id="errorOut" class="error-box" hidden></pre>
    </div>

    <div class="panel admin-panel">
      <div class="panel-head">
        <h2>Public settings</h2>
        <span id="configState" class="status-pill">Loading</span>
      </div>
      <form id="configForm" class="admin-form">
        <div class="settings-row compact-row">
          <label class="short-field">Public port
            <input id="publicPort" name="publicPort" type="number" min="1" max="65535" placeholder="9999">
          </label>
          <label class="medium-field">Bind address
            <input id="standaloneHTTPListen" name="standaloneHTTPListen" placeholder=":9999">
          </label>
        </div>
        <label class="url-field">Public base URL
          <input id="publicBaseURL" name="publicBaseURL" placeholder="http://localhost:9999">
        </label>
        <label class="password-field">Catalog password
          <input id="catalogPassword" name="catalogPassword" type="password" autocomplete="new-password" placeholder="Keep current if blank">
        </label>
        <div class="settings-row compact-row">
          <label class="short-field">Ebook source ID
            <input id="ebookInstallationID" name="ebookInstallationID" inputmode="numeric">
          </label>
          <label class="short-field">Audio source ID
            <input id="audioInstallationID" name="audioInstallationID" inputmode="numeric">
          </label>
        </div>
        <button class="button primary compact-action" type="submit">Save</button>
      </form>
      <pre id="configError" class="error-box" hidden></pre>
    </div>

    <div class="panel admin-panel">
      <div class="panel-head">
        <h2>HTML section editor</h2>
        <span id="htmlState" class="status-pill">Loading</span>
      </div>
      <form id="htmlForm" class="admin-form">
        <label>Published landing-page HTML
          <textarea id="htmlInput" rows="14" spellcheck="false"></textarea>
        </label>
        <div class="actions">
          <button class="button primary" type="submit">Publish HTML</button>
          <button class="button" id="previewHtml" type="button">Preview</button>
        </div>
      </form>
      <div class="preview-box" id="htmlPreview"></div>
    </div>

    <aside class="panel admin-panel">
      <h2>Status</h2>
      <dl class="details">
        <dt>Token secret</dt><dd>`+html.EscapeString(secretStatus)+`</dd>
        <dt>Catalog password</dt><dd>`+html.EscapeString(passwordStatus)+`</dd>
        <dt>Public listener</dt><dd>`+html.EscapeString(listenAddress)+`</dd>
        <dt>Public base URL</dt><dd>`+html.EscapeString(baseURL)+`</dd>
        <dt>Trusted HTML</dt><dd>Operator-provided landing-page HTML only; never paste user content.</dd>
        <dt>Extra sources</dt><dd>`+strconv.Itoa(len(d.Sources))+` configured</dd>
      </dl>
      <button class="button" id="refreshStats" type="button">Refresh stats</button>
      <div id="statsOut" class="stats slim"><div><strong>...</strong><span>Loading</span></div></div>
    </aside>

    <div class="panel admin-panel saved-links-panel">
      <div class="panel-head">
        <h2>Saved custom links</h2>
        <span id="linksState" class="status-pill">Loading</span>
      </div>
      <div id="savedLinks" class="saved-links"></div>
    </div>
  </section>
</main>
<script>
const params=new URLSearchParams(location.search);
const hostToken=params.get("token")||"";
if(params.has("token")){params.delete("token");history.replaceState(null,"",location.pathname+(params.toString()?"?"+params.toString():"")+location.hash)}
function headers(){const h={"Content-Type":"application/json"};if(hostToken)h.Authorization="Bearer "+hostToken;return h}
function list(name){return [...document.querySelectorAll('[name="'+name+'"]:checked')].map(el=>el.value)}
function csv(id){return document.getElementById(id).value.split(",").map(v=>v.trim()).filter(Boolean)}
function setState(text,bad=false){state.textContent=text;state.className=bad?"status-pill bad":"status-pill"}
function showError(message){errorOut.hidden=false;errorOut.textContent=message;setState("Error",true)}
function hideError(){errorOut.hidden=true;errorOut.textContent=""}
function absoluteURL(v){return new URL(v,location.href).toString()}
function customPageURL(name){
  if(!name)return "";
  const u=new URL(openPublicPage.href||"./",location.href);
  u.searchParams.set("page",name);
  return u.toString();
}
async function copyText(value,stateEl,message){
  await navigator.clipboard.writeText(value);
  stateEl.textContent=message;
  stateEl.className="status-pill";
}
async function readJSON(r){
  const text=await r.text();
  if(!text)return {};
  try{return JSON.parse(text)}catch(err){throw new Error("Expected JSON but received: "+text.slice(0,120))}
}
tokenForm.addEventListener("submit",async event=>{
  event.preventDefault(); hideError(); setState("Generating");
  const scope=document.querySelector('[name="tokenScope"]:checked')?.value||"all";
  const name=saveName.value.trim();
  const body={mediaTypes:scope==="all"?[]:list("mediaTypes"),libraryIds:csv("libraryIds"),saveName:name,html:customHTML.value};
  try{
    const r=await fetch("api/admin/catalog-token",{method:"POST",headers:headers(),body:JSON.stringify(body)});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Request failed");
    result.hidden=false; urlOut.value=absoluteURL(data.url); openLink.href=urlOut.value;
    pageUrlOut.value=name?customPageURL(name):""; openPageLink.href=pageUrlOut.value||"#";
    setState(name?"Saved":"Generated");
    await loadSavedLinks();
  }catch(err){showError(err.message||String(err))}
});
copy.addEventListener("click",async()=>{await copyText(urlOut.value,state,"Copied catalog URL")});
copyPage.addEventListener("click",async()=>{if(pageUrlOut.value)await copyText(pageUrlOut.value,state,"Copied page URL")});
function syncTokenScope(){const custom=document.querySelector('[name="tokenScope"]:checked')?.value==="custom";document.querySelectorAll('[name="mediaTypes"]').forEach(el=>{el.disabled=!custom})}
document.querySelectorAll('[name="tokenScope"]').forEach(el=>el.addEventListener("change",syncTokenScope));
syncTokenScope();
function valueOf(obj,...keys){for(const key of keys){if(obj&&obj[key]!==undefined&&obj[key]!==null)return obj[key]}return ""}
function setConfigState(text,bad=false){configState.textContent=text;configState.className=bad?"status-pill bad":"status-pill"}
async function loadConfig(){
  try{
    const r=await fetch("api/admin/config",{headers:headers()});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Config unavailable");
    publicPort.value=valueOf(data,"PublicPort","publicPort")||"";
    standaloneHTTPListen.value=valueOf(data,"StandaloneHTTPListen","standaloneHTTPListen")||"";
    publicBaseURL.value=valueOf(data,"PublicBaseURL","publicBaseURL")||"";
    ebookInstallationID.value=valueOf(data,"EbookInstallationID","ebookInstallationID")||"";
    audioInstallationID.value=valueOf(data,"AudioInstallationID","audioInstallationID")||"";
    setConfigState("Ready");
  }catch(err){setConfigState("Error",true);configError.hidden=false;configError.textContent=err.message||String(err)}
}
configForm.addEventListener("submit",async event=>{
  event.preventDefault(); configError.hidden=true; configError.textContent=""; setConfigState("Saving");
  const port=Number(publicPort.value||0);
  const body={
    PublicPort:Number.isFinite(port)?port:0,
    StandaloneHTTPListen:standaloneHTTPListen.value.trim(),
    PublicBaseURL:publicBaseURL.value.trim(),
    CatalogPassword:catalogPassword.value,
    EbookInstallationID:ebookInstallationID.value.trim(),
    AudioInstallationID:audioInstallationID.value.trim()
  };
  try{
    const r=await fetch("api/admin/config",{method:"PATCH",headers:headers(),body:JSON.stringify(body)});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Save failed");
    catalogPassword.value=""; setConfigState("Saved");
    const base=body.PublicBaseURL||publicURLFromListen(body.StandaloneHTTPListen||((body.PublicPort>0)?":"+body.PublicPort:""));
    if(base)openPublicPage.href=base;
  }catch(err){setConfigState("Error",true);configError.hidden=false;configError.textContent=err.message||String(err)}
});
function publicURLFromListen(listen){
  if(!listen)return "";
  let host=location.hostname, port="";
  const m=listen.match(/^\[?([^\]]*)\]?:(\d+)$/);
  if(m){if(m[1]&&m[1]!=="0.0.0.0"&&m[1]!=="::")host=m[1];port=m[2]}
  else if(/^\d+$/.test(listen)){port=listen}
  if(!port)return "";
  return location.protocol+"//"+host+":"+port+"/";
}
async function loadHTML(){
  try{
    const r=await fetch("api/admin/html-section",{headers:headers()});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"HTML unavailable");
    htmlInput.value=data.html||""; htmlPreview.innerHTML=htmlInput.value; htmlState.textContent="Ready";
  }catch(err){htmlState.textContent="Error"; htmlState.className="status-pill bad"; htmlPreview.textContent=err.message||String(err)}
}
htmlForm.addEventListener("submit",async event=>{
  event.preventDefault(); htmlState.textContent="Publishing"; htmlState.className="status-pill";
  try{
    const r=await fetch("api/admin/html-section",{method:"PUT",headers:headers(),body:JSON.stringify({html:htmlInput.value})});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Save failed");
    htmlPreview.innerHTML=htmlInput.value; htmlState.textContent="Published";
  }catch(err){htmlState.textContent="Error"; htmlState.className="status-pill bad"; htmlPreview.textContent=err.message||String(err)}
});
previewHtml.addEventListener("click",()=>{htmlPreview.innerHTML=htmlInput.value});
async function loadStats(){
  try{
    const r=await fetch("api/public/stats");
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Stats unavailable");
    statsOut.innerHTML="";
    const counts=data.mediaTypeCounts||[];
    if(!counts.length){statsOut.innerHTML='<div><strong>'+Number(data.totalItems||0)+'</strong><span>Total items</span></div>';return}
    for(const item of counts){const div=document.createElement("div");div.innerHTML='<strong>'+Number(item.count||0)+'</strong><span>'+esc(item.mediaType||"items")+'</span>';statsOut.appendChild(div)}
  }catch(err){statsOut.innerHTML='<div><strong>!</strong><span>'+esc(err.message||"Unavailable")+'</span></div>'}
}
function esc(s){return String(s??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
refreshStats.addEventListener("click",loadStats);
function setLinksState(text,bad=false){linksState.textContent=text;linksState.className=bad?"status-pill bad":"status-pill"}
function selectSavedLink(link){
  saveName.value=link.name||"";
  customHTML.value=link.html||"";
  libraryIds.value=(link.libraryIds||[]).join(",");
  document.querySelector('[name="tokenScope"][value="'+((link.mediaTypes||[]).length?"custom":"all")+'"]').checked=true;
  document.querySelectorAll('[name="mediaTypes"]').forEach(el=>{el.checked=(link.mediaTypes||[]).includes(el.value)});
  syncTokenScope();
  result.hidden=false;
  urlOut.value=absoluteURL(link.url||("catalog?token="+encodeURIComponent(link.token||"")));
  openLink.href=urlOut.value;
  pageUrlOut.value=customPageURL(link.name||"");
  openPageLink.href=pageUrlOut.value||"#";
  setState("Loaded");
}
async function deleteSavedLink(id){
  setLinksState("Deleting");
  const r=await fetch("api/admin/catalog-links/"+encodeURIComponent(id),{method:"DELETE",headers:headers()});
  const data=await readJSON(r);
  if(!r.ok) throw new Error(data?.error?.message||"Delete failed");
  await loadSavedLinks();
}
async function loadSavedLinks(){
  try{
    setLinksState("Loading");
    const r=await fetch("api/admin/catalog-links",{headers:headers()});
    const data=await readJSON(r);
    if(!r.ok) throw new Error(data?.error?.message||"Saved links unavailable");
    savedLinks.innerHTML="";
    if(!data.length){savedLinks.innerHTML='<p class="empty-note">No custom links saved yet.</p>';setLinksState("Ready");return}
    for(const link of data){
      const row=document.createElement("div");
      row.className="saved-link-row";
      const page=customPageURL(link.name||"");
      row.innerHTML='<div class="saved-link-copy"><strong>'+esc(link.name||"Untitled")+'</strong><span>'+esc(page)+'</span></div><div class="saved-link-actions"><button class="button" type="button" data-act="load">Load</button><button class="button" type="button" data-act="copy">Copy</button><a class="button" data-act="open" target="_blank" rel="noreferrer">Open</a><button class="button danger" type="button" data-act="delete">Delete</button></div>';
      row.querySelector('[data-act="open"]').href=page;
      row.querySelector('[data-act="load"]').addEventListener("click",()=>selectSavedLink(link));
      row.querySelector('[data-act="copy"]').addEventListener("click",async()=>copyText(page,linksState,"Copied"));
      row.querySelector('[data-act="delete"]').addEventListener("click",async()=>{try{await deleteSavedLink(link.id)}catch(err){setLinksState("Error",true);alert(err.message||String(err))}});
      savedLinks.appendChild(row);
    }
    setLinksState("Ready");
  }catch(err){setLinksState("Error",true);savedLinks.innerHTML='<p class="empty-note">'+esc(err.message||String(err))+'</p>'}
}
loadStats(); loadHTML(); loadConfig(); loadSavedLinks();
</script>
</body>
</html>`)
	}
}

func publicBaseURLForRequest(r *http.Request, d Deps) string {
	if d.PublicBaseURL != "" {
		return strings.TrimRight(d.PublicBaseURL, "/") + "/"
	}
	if d.StandaloneListen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(d.StandaloneListen)
	if err != nil {
		if strings.HasPrefix(d.StandaloneListen, ":") {
			port = strings.TrimPrefix(d.StandaloneListen, ":")
		} else if _, convErr := strconv.Atoi(d.StandaloneListen); convErr == nil {
			port = d.StandaloneListen
		} else {
			return ""
		}
	}
	if port == "" {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = requestHostname(r)
	}
	if host == "" {
		host = "localhost"
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/"
}

func requestHostname(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		return h
	}
	return strings.Trim(r.Host, "[]")
}

func publicCatalogStats(ctx context.Context, d Deps, libraryIDs []string) (*store.CatalogStats, error) {
	if d.ConfigStore != nil {
		stats, err := d.ConfigStore.CatalogStats(ctx, libraryIDs)
		if err == nil && stats != nil && (stats.TotalItems > 0 || len(stats.MediaTypeCounts) > 0 || len(stats.LibraryCounts) > 0 || len(stats.QualityCounts) > 0) {
			host := currentHost(d)
			if host != nil {
				if withSources, sourceErr := withSourceStats(ctx, host, runtimeStatsFromStore(stats), d.Sources); sourceErr == nil {
					stats = storeStatsFromRuntime(withSources, stats.QualityCounts)
				}
			}
			return stats, nil
		}
	}
	stats, err := hostStats(ctx, d, libraryIDs)
	if err != nil {
		return nil, err
	}
	return storeStatsFromRuntime(stats, nil), nil
}

func hostStats(ctx context.Context, d Deps, libraryIDs []string) (*runtimehost.CatalogStats, error) {
	if d.statsCache != nil {
		return d.statsCache.get(ctx, d.Host, libraryIDs, d.Sources)
	}
	host := currentHost(d)
	if host == nil {
		return nil, http.ErrServerClosed
	}
	stats, err := safeGetCatalogStats(ctx, host, libraryIDs)
	if err != nil {
		return nil, err
	}
	return withSourceStats(ctx, host, stats, d.Sources)
}

func runtimeStatsFromStore(stats *store.CatalogStats) *runtimehost.CatalogStats {
	if stats == nil {
		return &runtimehost.CatalogStats{}
	}
	out := &runtimehost.CatalogStats{TotalItems: stats.TotalItems}
	for _, c := range stats.MediaTypeCounts {
		out.MediaTypeCounts = append(out.MediaTypeCounts, runtimehost.CatalogTypeCount{MediaType: c.MediaType, Count: c.Count})
	}
	for _, c := range stats.LibraryCounts {
		out.LibraryCounts = append(out.LibraryCounts, runtimehost.CatalogLibraryCount{
			LibraryID: c.LibraryID, LibraryName: c.LibraryName, MediaType: c.MediaType, Count: c.Count,
		})
	}
	return out
}

func storeStatsFromRuntime(stats *runtimehost.CatalogStats, quality []store.CatalogQualityCount) *store.CatalogStats {
	if stats == nil {
		return &store.CatalogStats{QualityCounts: quality}
	}
	out := &store.CatalogStats{TotalItems: stats.TotalItems, QualityCounts: quality}
	for _, c := range stats.MediaTypeCounts {
		out.MediaTypeCounts = append(out.MediaTypeCounts, store.CatalogTypeCount{MediaType: c.MediaType, Count: c.Count})
	}
	for _, c := range stats.LibraryCounts {
		out.LibraryCounts = append(out.LibraryCounts, store.CatalogLibraryCount{
			LibraryID: c.LibraryID, LibraryName: c.LibraryName, MediaType: c.MediaType, Count: c.Count,
		})
	}
	return out
}

type statsCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]statsCacheEntry
}

type statsCacheEntry struct {
	expires time.Time
	stats   *runtimehost.CatalogStats
}

type catalogCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	media   map[string]catalogMediaCacheEntry
	filters map[string]catalogFiltersCacheEntry
}

type catalogMediaCacheEntry struct {
	expires time.Time
	resp    *store.CatalogMediaResponse
}

type catalogFiltersCacheEntry struct {
	expires time.Time
	resp    *store.CatalogFilters
}

func newCatalogCache(ttl time.Duration) *catalogCache {
	return &catalogCache{
		ttl:     ttl,
		media:   map[string]catalogMediaCacheEntry{},
		filters: map[string]catalogFiltersCacheEntry{},
	}
}

func (c *catalogCache) getMedia(key string) (*store.CatalogMediaResponse, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.media[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(c.media, key)
		}
		return nil, false
	}
	return entry.resp, true
}

func (c *catalogCache) setMedia(key string, resp *store.CatalogMediaResponse) {
	if c == nil || key == "" || resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.media[key] = catalogMediaCacheEntry{expires: time.Now().Add(c.ttl), resp: resp}
	if len(c.media) > 256 {
		c.pruneLocked()
	}
}

func (c *catalogCache) getFilters(key string) (*store.CatalogFilters, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.filters[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(c.filters, key)
		}
		return nil, false
	}
	return entry.resp, true
}

func (c *catalogCache) setFilters(key string, resp *store.CatalogFilters) {
	if c == nil || key == "" || resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filters[key] = catalogFiltersCacheEntry{expires: time.Now().Add(c.ttl), resp: resp}
	if len(c.filters) > 128 {
		c.pruneLocked()
	}
}

func (c *catalogCache) pruneLocked() {
	now := time.Now()
	for key, entry := range c.media {
		if now.After(entry.expires) {
			delete(c.media, key)
		}
	}
	for key, entry := range c.filters {
		if now.After(entry.expires) {
			delete(c.filters, key)
		}
	}
}

func catalogMediaCacheKey(libraryIDs, mediaTypes []string, query, genre string, yearMin, yearMax int, sort string, descending bool, pageSize int, pageToken string) string {
	return strings.Join([]string{
		"media",
		cacheListKey(libraryIDs),
		cacheListKey(mediaTypes),
		strings.TrimSpace(query),
		strings.TrimSpace(genre),
		strconv.Itoa(yearMin),
		strconv.Itoa(yearMax),
		sort,
		strconv.FormatBool(descending),
		strconv.Itoa(pageSize),
		pageToken,
	}, "\x00")
}

func catalogFiltersCacheKey(libraryIDs, mediaTypes []string) string {
	return strings.Join([]string{"filters", cacheListKey(libraryIDs), cacheListKey(mediaTypes)}, "\x00")
}

func cacheListKey(in []string) string {
	out := cleanList(in)
	sort.Strings(out)
	return strings.Join(out, ",")
}

func (c *statsCache) get(ctx context.Context, hostFn func() Host, libraryIDs []string, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	key := statsCacheKey(libraryIDs)
	now := time.Now()
	c.mu.Lock()
	if c.entries != nil {
		if ent, ok := c.entries[key]; ok && now.Before(ent.expires) {
			stats := ent.stats
			c.mu.Unlock()
			return stats, nil
		}
	}
	c.mu.Unlock()

	host := currentHost(Deps{Host: hostFn})
	if host == nil {
		return nil, http.ErrServerClosed
	}
	stats, err := safeGetCatalogStats(ctx, host, libraryIDs)
	if err != nil {
		return nil, err
	}
	stats, err = withSourceStats(ctx, host, stats, sources)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]statsCacheEntry{}
	}
	c.entries[key] = statsCacheEntry{expires: now.Add(c.ttl), stats: stats}
	c.mu.Unlock()
	return stats, nil
}

func safeGetCatalogStats(ctx context.Context, host Host, libraryIDs []string) (stats *runtimehost.CatalogStats, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stats = nil
			err = fmt.Errorf("get catalog stats panicked: %v", recovered)
		}
	}()
	return host.GetCatalogStats(ctx, libraryIDs)
}

func currentHost(d Deps) Host {
	if d.Host == nil {
		return nil
	}
	return d.Host()
}

func withSourceStats(ctx context.Context, host Host, base *runtimehost.CatalogStats, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	if base == nil {
		base = &runtimehost.CatalogStats{}
	}
	for _, src := range sources {
		if src == nil {
			continue
		}
		stats, err := src.Stats(ctx, host)
		if err != nil {
			continue
		}
		base.TotalItems += stats.TotalItems
		base.MediaTypeCounts = append(base.MediaTypeCounts, stats.MediaTypeCounts...)
		base.LibraryCounts = append(base.LibraryCounts, stats.LibraryCounts...)
	}
	return base, nil
}

func appendSourceCatalogs(ctx context.Context, host Host, base *runtimehost.ListLibraryMediaResponse, sources []CatalogSource, req runtimehost.ListLibraryMediaRequest) *runtimehost.ListLibraryMediaResponse {
	if base == nil {
		base = &runtimehost.ListLibraryMediaResponse{}
	}
	for _, src := range sources {
		if src == nil || !mediaTypeRequested(req.MediaTypes, src.MediaType()) {
			continue
		}
		sourceReq := req
		sourceReq.MediaTypes = []string{src.MediaType()}
		if sourceReq.PageSize <= 0 || sourceReq.PageSize > 12 {
			sourceReq.PageSize = 12
		}
		resp, err := src.List(ctx, host, sourceReq)
		if err != nil || resp == nil {
			continue
		}
		base.Items = append(base.Items, resp.Items...)
		base.TotalCount += resp.TotalCount
	}
	return base
}

func mediaTypeRequested(requested []string, mediaType string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, v := range requested {
		if v == mediaType {
			return true
		}
	}
	return false
}

func statsCacheKey(libraryIDs []string) string {
	ids := cleanList(libraryIDs)
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
}

func statsHTML(stats *store.CatalogStats) string {
	if stats == nil {
		return `<p>Stats are not available yet.</p>`
	}
	var b strings.Builder
	b.WriteString(`<div class="stats">`)
	b.WriteString(`<div class="stat-card stat-total"><small>Total</small><strong>`)
	b.WriteString(formatCount(stats.TotalItems))
	b.WriteString(`</strong><span>Total items</span></div>`)
	if len(stats.MediaTypeCounts) == 0 && len(stats.LibraryCounts) == 0 && len(stats.QualityCounts) == 0 {
		b.WriteString(`</div>`)
		return b.String()
	}
	for _, c := range stats.LibraryCounts {
		b.WriteString(`<div class="stat-card stat-library"><small>Library</small><strong>`)
		b.WriteString(formatCount(c.Count))
		b.WriteString(`</strong><span>`)
		b.WriteString(html.EscapeString(c.LibraryName))
		b.WriteString(`</span></div>`)
	}
	for _, c := range stats.MediaTypeCounts {
		b.WriteString(`<div class="stat-card stat-media"><small>Media</small><strong>`)
		b.WriteString(formatCount(c.Count))
		b.WriteString(`</strong><span>`)
		b.WriteString(html.EscapeString(c.MediaType))
		b.WriteString(`</span></div>`)
	}
	for _, c := range stats.QualityCounts {
		b.WriteString(`<div class="stat-card stat-quality"><small>Quality</small><strong>`)
		b.WriteString(formatCount(c.Count))
		b.WriteString(`</strong><span>`)
		b.WriteString(html.EscapeString(c.Label))
		b.WriteString(`</span></div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func formatCount(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(s[:first])
	for i := first; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func adHTML(v string) string {
	if strings.TrimSpace(v) == "" {
		return `<div class="custom-html-sample"><p><strong>Private catalog access</strong></p><p>Use the searchable catalog to confirm availability before requesting access. Curated public links can include a dedicated note for each recipient, library group, or event.</p></div>`
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func boundedInt(raw string, fallback, min, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func defaultPositive(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mergeMediaTypes(allowed, requested []string) ([]string, bool) {
	requested, requestedOK := cleanMediaTypes(requested)
	if !requestedOK {
		return nil, false
	}
	allowed, allowedOK := cleanMediaTypes(allowed)
	if !allowedOK {
		return nil, false
	}
	if len(allowed) == 0 {
		return requested, true
	}
	if len(requested) == 0 {
		return allowed, true
	}
	ok := map[string]bool{}
	for _, v := range allowed {
		ok[v] = true
	}
	out := []string{}
	for _, v := range requested {
		if ok[v] {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

func mergeLibraryIDs(allowed, requested []string) ([]string, bool) {
	requested = cleanList(requested)
	allowed = cleanList(allowed)
	if len(allowed) == 0 {
		return requested, true
	}
	if len(requested) == 0 {
		return allowed, true
	}
	ok := map[string]bool{}
	for _, v := range allowed {
		ok[v] = true
	}
	out := []string{}
	for _, v := range requested {
		if ok[v] {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

func cleanMediaTypes(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, v := range cleanList(in) {
		switch strings.ToLower(v) {
		case "movie":
			out = append(out, "movie")
		case "tv", "series":
			out = append(out, "tv")
		case "episode":
			out = append(out, "episode")
		case "audiobook":
			out = append(out, "audiobook")
		case "ebook":
			out = append(out, "ebook")
		default:
			return nil, false
		}
	}
	return out, true
}

func css() string {
	return `:root{color-scheme:dark;--bg:#0d1016;--fg:#f6f7fb;--muted:#a6b0c0;--muted2:#788396;--panel:#151923;--panel2:#111720;--border:#283142;--input:#121821;--accent:#d5ae63;--accent2:#7c9fb6;--primary:#d5ae63;--primary-border:#e1c37d;--shadow:rgba(5,8,14,.38)}[data-theme="cinema-light"]{color-scheme:light;--bg:#f4f1ea;--fg:#1c1b18;--muted:#6f695f;--muted2:#91877a;--panel:#fffaf1;--panel2:#f9f4ea;--border:#ded2be;--input:#fffaf1;--accent:#99642a;--accent2:#526f7b;--primary:#99642a;--primary-border:#b9823c;--shadow:rgba(80,54,24,.16)}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#9fb2d0;--muted2:#8aa0c0;--panel:#172033;--panel2:#121c2d;--border:#2d3f61;--input:#121c2f;--accent:#d5ae63;--accent2:#7aa5d8;--primary:#d5ae63;--primary-border:#e6c879;--shadow:rgba(4,9,18,.38)}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#c59aa6;--muted2:#a77d89;--panel:#241018;--panel2:#1c0d13;--border:#4a2230;--input:#211018;--accent:#e0b069;--accent2:#ca788d;--primary:#e0b069;--primary-border:#f0c57c;--shadow:rgba(23,4,10,.45)}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bc7b2;--muted2:#7da18f;--panel:#14241b;--panel2:#101e16;--border:#2b4b39;--input:#102017;--accent:#d1b46c;--accent2:#7bb69a;--primary:#d1b46c;--primary-border:#e3ca7d;--shadow:rgba(4,16,10,.42)}*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;font-family:Geist,Satoshi,Outfit,system-ui,-apple-system,Segoe UI,sans-serif;background:radial-gradient(circle at 80% -10%,color-mix(in srgb,var(--accent) 16%,transparent),transparent 36rem),linear-gradient(180deg,var(--bg),color-mix(in srgb,var(--bg) 82%,#02040a));color:var(--fg);font-variant-numeric:tabular-nums}body:before{content:"";position:fixed;inset:0;pointer-events:none;opacity:.035;background-image:linear-gradient(90deg,var(--fg) 1px,transparent 1px),linear-gradient(var(--fg) 1px,transparent 1px);background-size:48px 48px;mask-image:linear-gradient(to bottom,#000,transparent 72%)}a{color:inherit}.skip-link{position:absolute;left:16px;top:12px;transform:translateY(-160%);background:var(--fg);color:var(--bg);padding:8px 10px;border-radius:6px}.skip-link:focus{transform:translateY(0)}.shell{max-width:1360px;margin:0 auto;padding:36px 24px}.public-shell{padding-bottom:68px}.page-header{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:28px;padding:34px 0 28px}.public-hero{min-height:clamp(520px,74dvh,760px);grid-template-columns:minmax(0,1.05fr) minmax(320px,.72fr);align-items:center;padding:42px 0 48px}.hero-copy{display:grid;gap:20px;max-width:760px}.space{display:grid;gap:8px}.eyebrow{margin:0;color:var(--accent);text-transform:uppercase;font-size:12px;font-weight:750;letter-spacing:.16em}.page-title{font-size:clamp(2.35rem,6vw,5.9rem);line-height:.94;margin:0;font-weight:760;letter-spacing:0;text-wrap:balance}.lead,.page-subtitle{color:var(--muted);font-size:clamp(1rem,1.45vw,1.16rem);line-height:1.65;margin:0;max-width:64ch;text-wrap:pretty}.hero-actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:6px}.result-count{text-align:right;min-width:176px}.result-count strong{display:block;font-size:clamp(2rem,4vw,4.4rem);line-height:.9;font-weight:330;letter-spacing:0}.result-count span{color:var(--muted);font-size:11px;font-weight:750;letter-spacing:.16em;text-transform:uppercase}.hero-visual{position:relative;min-height:390px;border:1px solid color-mix(in srgb,var(--border) 80%,transparent);border-radius:8px;background:linear-gradient(145deg,color-mix(in srgb,var(--panel) 96%,transparent),color-mix(in srgb,var(--panel2) 86%,transparent));box-shadow:0 28px 72px -42px var(--shadow);overflow:hidden}.hero-count{position:absolute;right:24px;top:24px;z-index:2}.preview-stack{position:absolute;left:32px;right:28px;bottom:28px;top:98px;display:grid;grid-template-columns:1.15fr .86fr;grid-template-rows:.8fr 1fr;gap:12px;transform:rotate(-2deg)}.preview-stack span{display:block;border:1px solid color-mix(in srgb,var(--border) 80%,transparent);border-radius:8px;background:linear-gradient(160deg,color-mix(in srgb,var(--accent2) 22%,var(--panel)),color-mix(in srgb,var(--accent) 16%,var(--panel2)));box-shadow:0 20px 44px -30px var(--shadow);animation:catalog-float 8s cubic-bezier(.16,1,.3,1) infinite}.preview-stack span:nth-child(1){grid-row:1/3}.preview-stack span:nth-child(2){animation-delay:-1.5s}.preview-stack span:nth-child(3){animation-delay:-3s}.preview-stack span:nth-child(4){display:none}@keyframes catalog-float{0%,100%{transform:translateY(0)}50%{transform:translateY(-8px)}}.surface-panel{background:color-mix(in srgb,var(--panel) 88%,transparent);border:1px solid var(--border);border-radius:8px;padding:18px;box-shadow:0 20px 60px -48px var(--shadow)}.panel{border-top:1px solid var(--border);padding:34px 0}.panel h2,.surface-panel h2{margin:0 0 18px;font-size:18px;font-weight:700;letter-spacing:0}.published-html{max-width:78ch;color:var(--muted);line-height:1.7}.published-html :first-child{margin-top:0}.published-html :last-child{margin-bottom:0}.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(148px,1fr));gap:12px}.stats div{position:relative;min-height:116px;background:linear-gradient(180deg,color-mix(in srgb,var(--panel) 96%,transparent),color-mix(in srgb,var(--panel2) 96%,transparent));border:1px solid color-mix(in srgb,var(--border) 92%,transparent);border-radius:8px;padding:14px;overflow:hidden;transition:transform .28s cubic-bezier(.16,1,.3,1),border-color .28s cubic-bezier(.16,1,.3,1)}.stats div:before{content:"";position:absolute;inset:0 0 auto;height:2px;background:var(--accent);opacity:.72}.stats div:hover{transform:translateY(-2px);border-color:color-mix(in srgb,var(--accent) 44%,var(--border))}.stats small{display:block;color:var(--muted2);font-size:10px;font-weight:760;letter-spacing:.16em;text-transform:uppercase}.stats strong{display:block;margin-top:18px;font-size:clamp(1.45rem,2.5vw,2.15rem);font-weight:620;line-height:1}.stats span,.media-card p{color:var(--muted)}.stats span{display:block;margin-top:8px;font-size:13px;line-height:1.3}.stats-hero{margin-bottom:18px}.toolbar{display:grid;grid-template-columns:minmax(220px,1fr) repeat(3,minmax(136px,160px)) repeat(3,minmax(112px,140px));gap:10px;position:sticky;top:0;z-index:5;margin-bottom:22px;backdrop-filter:blur(18px);box-shadow:0 18px 54px -46px var(--shadow)}input,select,.button{border:1px solid var(--border);background:color-mix(in srgb,var(--input) 94%,transparent);color:var(--fg);border-radius:6px;padding:10px 12px;font:inherit;min-height:42px;transition:transform .24s cubic-bezier(.16,1,.3,1),border-color .24s cubic-bezier(.16,1,.3,1),background .24s cubic-bezier(.16,1,.3,1)}input:focus,select:focus,.button:focus-visible{outline:2px solid color-mix(in srgb,var(--accent) 70%,transparent);outline-offset:2px}.button{display:inline-flex;align-items:center;justify-content:center;gap:8px;text-decoration:none;cursor:pointer;font-weight:720}.button:hover{transform:translateY(-1px);border-color:color-mix(in srgb,var(--accent) 46%,var(--border))}.button:active{transform:translateY(1px) scale(.99)}.button.primary,.primary{background:var(--primary);border-color:var(--primary-border);color:#15110a}.button.secondary{background:transparent}.poster-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}.media-card{min-width:0;animation:card-in .42s cubic-bezier(.16,1,.3,1) both}.media-card-image{position:relative;aspect-ratio:2/3;background:var(--panel);border:1px solid var(--border);border-radius:8px;overflow:hidden;box-shadow:0 22px 44px -34px var(--shadow);transition:transform .3s cubic-bezier(.16,1,.3,1),border-color .3s cubic-bezier(.16,1,.3,1)}.media-card:hover .media-card-image{transform:translateY(-4px);border-color:color-mix(in srgb,var(--accent) 38%,var(--border))}.media-card-image img{width:100%;height:100%;object-fit:cover;display:block}.poster-gradient{position:absolute;inset:auto 0 0;height:96px;background:linear-gradient(to top,rgba(5,7,12,.62),transparent);pointer-events:none}.poster-fallback{height:100%;display:flex;align-items:center;justify-content:center;text-align:center;padding:12px;color:var(--muted);font-size:14px;font-weight:650}.media-card-copy{padding:10px 2px 0}.media-card-copy h3{font-size:14px;line-height:1.25;margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-weight:680}.media-card-copy p{font-size:12px;line-height:1.35;margin:5px 0 0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.media-card-copy .meta{font-size:11px;font-weight:760;letter-spacing:.12em;text-transform:uppercase}.empty-state{grid-column:1/-1;color:var(--muted);text-align:center;padding:54px 16px;border:1px dashed var(--border);border-radius:8px}.load-row{display:flex;justify-content:center;padding:30px 0 8px}@keyframes card-in{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:translateY(0)}}@media(min-width:640px){.poster-grid{grid-template-columns:repeat(4,minmax(0,1fr))}}@media(min-width:768px){.poster-grid{grid-template-columns:repeat(5,minmax(0,1fr))}}@media(min-width:1024px){.poster-grid{grid-template-columns:repeat(7,minmax(0,1fr))}}@media(min-width:1280px){.poster-grid{grid-template-columns:repeat(8,minmax(0,1fr))}}@media(max-width:900px){.shell{padding:28px 16px}.page-header,.public-hero{display:grid;grid-template-columns:1fr;min-height:auto}.hero-visual{min-height:280px}.result-count{text-align:left}.hero-count{left:18px;right:auto}.preview-stack{left:18px;right:18px;top:94px}.toolbar{grid-template-columns:1fr 1fr;position:static}.toolbar input:first-child{grid-column:1/-1}}@media(max-width:560px){.toolbar{grid-template-columns:1fr}.poster-grid{gap:10px}.media-card-copy h3{font-size:13px}.hero-actions .button{width:100%}.stats{grid-template-columns:1fr 1fr}.stats div{min-height:104px}}`
}

func adminCSS() string {
	return `.admin-shell{max-width:1220px}.admin-hero{padding-bottom:24px}.admin-grid{display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:24px}.admin-panel{border:1px solid var(--border);border-radius:8px;background:var(--panel2);padding:20px}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:12px}.admin-panel h2{margin:0 0 16px}.admin-form{display:grid;gap:16px;justify-items:start}.admin-form label{display:grid;gap:7px;color:var(--muted);width:100%;max-width:100%}.admin-form fieldset{border:1px solid var(--border);border-radius:8px;margin:0;padding:14px;display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px;width:100%;box-sizing:border-box}.admin-form legend{color:var(--muted);padding:0 6px}.admin-form input,.admin-form textarea{box-sizing:border-box;width:100%}.settings-row{display:flex;flex-wrap:wrap;align-items:end;gap:12px;width:100%}.compact-row{max-width:560px}.short-field{max-width:150px}.medium-field{max-width:220px}.url-field{max-width:420px}.password-field{max-width:280px}.compact-action{width:auto;min-width:92px;justify-self:start}.check{display:flex!important;align-items:center;gap:8px}.check input{width:auto}.primary{background:var(--primary);border-color:var(--primary-border)}.danger{border-color:#7f2d2d;color:#ffb4b4}.status-pill{display:inline-flex;align-items:center;border:1px solid var(--primary-border);border-radius:999px;color:var(--accent);padding:6px 10px;font-size:12px}.status-pill.bad{border-color:#b54747;color:#ffadad}.result-box,.error-box{margin-top:18px}.result-box textarea{width:100%;box-sizing:border-box;resize:vertical}.actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:10px}.details{display:grid;grid-template-columns:130px 1fr;gap:10px;margin:0 0 16px}.details dt{color:var(--muted2)}.details dd{margin:0;color:var(--fg);overflow-wrap:anywhere}.compact{margin-top:12px}.error-box{white-space:pre-wrap;border:1px solid #6b2d2d;background:#2b1518;color:#ffc4c4;border-radius:8px;padding:12px}.preview-box{min-height:120px;margin-top:14px;border:1px solid var(--border);border-radius:6px;background:var(--panel);padding:14px;overflow:auto}.slim{grid-template-columns:1fr 1fr}.admin-form .slim{display:grid;gap:12px}.saved-links-panel{grid-column:1/-1}.saved-links{display:grid;gap:10px}.saved-link-row{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:center;border:1px solid var(--border);border-radius:8px;background:var(--panel);padding:12px}.saved-link-copy{min-width:0;display:grid;gap:4px}.saved-link-copy strong{font-size:15px}.saved-link-copy span,.empty-note{color:var(--muted);overflow-wrap:anywhere}.saved-link-actions{display:flex;flex-wrap:wrap;gap:8px;justify-content:flex-end}.saved-link-actions .button{min-height:36px;padding:7px 10px}@media(max-width:900px){.admin-grid{grid-template-columns:1fr}.slim{grid-template-columns:repeat(auto-fit,minmax(140px,1fr))}.saved-link-row{grid-template-columns:1fr}.saved-link-actions{justify-content:flex-start}}@media(max-width:560px){.short-field,.medium-field,.url-field,.password-field{max-width:100%}.compact-action{width:100%}}`
}

func adminTheme(r *http.Request) string {
	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = r.Header.Get("X-Continuum-Theme")
	}
	if theme == "" {
		theme = r.Header.Get("X-Continuum-User-Theme")
	}
	if theme == "" {
		theme = "default"
	}
	return html.EscapeString(theme)
}
