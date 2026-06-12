package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimehost"
)

// hCatalogMedia is the gated catalog browse endpoint. Source preference:
//  1. Local store (DB fast path) for movie/series/episode requests
//  2. Federated CatalogSource for single-type ebook/audiobook requests
//  3. Host SDK ListLibraryMedia for the general case
//
// Returns ListLibraryMediaResponse shape in all paths so the SPA can
// consume one schema regardless of which source served it.
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
				writeInternal(w, r, d, "catalog_failed", err)
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
			empty := emptyCatalogMediaResponse()
			writeJSON(w, http.StatusOK, empty)
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
			empty := emptyCatalogMediaResponse()
			writeJSON(w, http.StatusOK, empty)
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
			writeInternal(w, r, d, "filters_failed", err)
			return
		}
		d.catalogCache.setFilters(cacheKey, filters)
		w.Header().Set("X-Public-Catalog-Cache", "MISS")
		writeJSON(w, http.StatusOK, filters)
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
			writeInternal(w, r, d, "seasons_failed", err)
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
			writeInternal(w, r, d, "episodes_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, episodes)
	}
}

// enrichCatalogItemImages calls into the host SDK to pick up provider
// poster/backdrop URLs for an item, since the local DB only stores
// internal paths.
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

// overlayCatalogMediaImages fills in any item.PosterURL/BackdropURL
// that came back blank from the store fast path by resolving the
// internal poster paths through the host SDK's image resolver.
func overlayCatalogMediaImages(ctx context.Context, d Deps, _ runtimehost.ListLibraryMediaRequest, items []store.CatalogMediaItem) {
	if len(items) == 0 || d.ConfigStore == nil {
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
