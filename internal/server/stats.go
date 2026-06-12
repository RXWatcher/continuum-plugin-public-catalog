package server

import (
	"context"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimehost"
)

func hStats(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /api/public/stats always reports unscoped (nil libraryIDs)
		// aggregate counts: the response is identical for every visitor
		// and carries no session-scoped data. For genuinely anonymous
		// requests we can therefore relax the blanket no-store and allow a
		// short shared-cache window matching the store-stats TTL. Any
		// request that DOES carry a catalog session/token keeps no-store,
		// since a shared cache must not serve a session-bearing response.
		if !hasCatalogSession(r) {
			setPublicStatsCacheControl(w, d.StatsCacheTTL)
		}
		stats, err := publicCatalogStats(r.Context(), d, nil)
		if err != nil {
			// Stats are best-effort on the public landing page — return
			// an empty payload instead of surfacing the failure.
			writeJSON(w, http.StatusOK, store.CatalogStats{})
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

// hasCatalogSession reports whether the request carries a catalog bypass
// token or session cookie. Used to keep session-bearing responses out of
// shared caches even on otherwise-public endpoints.
func hasCatalogSession(r *http.Request) bool {
	if strings.TrimSpace(r.URL.Query().Get("token")) != "" {
		return true
	}
	if cookie, err := r.Cookie("public_catalog_auth"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return true
	}
	return false
}

// setPublicStatsCacheControl stamps a short shared-cache window for the
// anonymous public stats response, overriding the middleware's blanket
// no-store. max-age tracks the store-stats TTL so a cached copy never
// outlives the server-side memoised value by much.
func setPublicStatsCacheControl(w http.ResponseWriter, ttl time.Duration) {
	secs := int(ttl.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(secs))
	w.Header().Set("Vary", "Cookie")
}

// publicCatalogStats reads catalog totals via the store fast path
// (direct DB) when available, falling back to the host SDK. Federated
// stats from companion plugin sources are added on top.
func publicCatalogStats(ctx context.Context, d Deps, libraryIDs []string) (*store.CatalogStats, error) {
	if d.ConfigStore != nil {
		cacheKey := statsCacheKey(libraryIDs)
		if cached, ok := d.storeStatsCache.get(cacheKey); ok {
			return cached, nil
		}
		stats, err := d.ConfigStore.CatalogStats(ctx, libraryIDs)
		if err == nil && stats != nil && (stats.TotalItems > 0 || len(stats.MediaTypeCounts) > 0 || len(stats.LibraryCounts) > 0 || len(stats.QualityCounts) > 0) {
			host := currentHost(d)
			if host != nil {
				if withSources, sourceErr := withSourceStats(ctx, host, runtimeStatsFromStore(stats), d.Sources); sourceErr == nil {
					stats = storeStatsFromRuntime(withSources, stats.QualityCounts)
				}
			}
			d.storeStatsCache.set(cacheKey, stats)
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

// scopeCatalogStats filters a stats payload to a single mediaType set.
// Used so a per-bypass-token scope on the catalog landing page hides
// rows that the recipient isn't allowed to see.
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

// statsHTML renders the stats payload as a small static HTML block used
// in fallback / non-SPA rendering paths and in tests.
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
