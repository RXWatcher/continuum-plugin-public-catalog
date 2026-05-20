package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/store"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

func hLanding(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customHTML := ""
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

// hAdminPage serves the SPA shell with mode="admin". The React admin
// page in web/src/pages/Admin.tsx reads the bootstrap and renders the
// settings / catalog-password / HTML / saved-links surfaces.
func hAdminPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writePublicApp(w, r, publicBootstrap{
			Mode:  "admin",
			Theme: adminTheme(r),
		}, http.StatusOK)
	}
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

// initialCatalogLibraryID picks the library id the catalog page should
// open with. Respects an explicit ?libraryId query param if allowed by
// the token scope; otherwise picks the first allowed library that has
// items.
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

// adHTML wraps the operator-published HTML in a fallback when nothing
// has been published. The fallback is plain English with no script
// tags so it remains safe to embed verbatim in the SPA bootstrap.
func adHTML(v string) string {
	if strings.TrimSpace(v) == "" {
		return `<div class="custom-html-sample"><p><strong>Private catalog access</strong></p><p>Use the searchable catalog to confirm availability before requesting access. Curated public links can include a dedicated note for each recipient, library group, or event.</p></div>`
	}
	return v
}

// publishedHTML returns the operator-edited HTML stored in the config
// row, falling back to the manifest-supplied AdHTML seed.
func publishedHTML(ctx context.Context, d Deps) string {
	if d.ConfigStore == nil {
		return ""
	}
	cfg, err := d.ConfigStore.GetConfig(ctx)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(cfg.PublishedHTML) != "" {
		return cfg.PublishedHTML
	}
	return cfg.AdHTML
}

// publicBaseURLForRequest derives the absolute URL clients should be
// pointed at. Prefers the explicit PublicBaseURL config; if the plugin
// runs in standalone-listener mode, synthesises the URL from the
// listener address + the request's host/scheme.
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
