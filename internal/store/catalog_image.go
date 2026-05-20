package store

import (
	"context"
	"fmt"
	"strings"
)

// ProviderImageURL looks up a cached host-provided poster/backdrop URL
// for the given item. Cache miss returns "" so callers can decide
// whether to enqueue an SDK refresh.
func (s *Store) ProviderImageURL(ctx context.Context, itemType, tmdbID, tvdbID, imageKind string, seasonNumber int) string {
	cacheKey := strings.Join([]string{itemType, tmdbID, tvdbID, imageKind, fmt.Sprintf("%d", seasonNumber)}, ":")
	return s.cachedProviderImage(ctx, cacheKey)
}

func (s *Store) cachedProviderImage(ctx context.Context, cacheKey string) string {
	var out string
	if err := s.pool.QueryRow(ctx,
		`SELECT url FROM provider_image_cache WHERE cache_key = $1 AND expires_at > NOW()`,
		cacheKey,
	).Scan(&out); err != nil {
		return ""
	}
	return out
}

// CatalogMediaImagePaths returns the host's provider-image paths (TMDB
// or TVDB paths) for a batch of content IDs, used by the overlay step
// that fills in missing poster/backdrop URLs before serving them to
// the catalog UI.
func (s *Store) CatalogMediaImagePaths(ctx context.Context, mediaIDs []string) (map[string]CatalogMediaImagePaths, error) {
	ids := cleanStrings(mediaIDs)
	if len(ids) == 0 {
		return map[string]CatalogMediaImagePaths{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH wanted AS (
			SELECT unnest($1::text[]) AS content_id
		)
		SELECT
			w.content_id,
			COALESCE(e.still_path, s.poster_path, mi.poster_path, '') AS poster_path,
			COALESCE(series_mi.backdrop_path, mi.backdrop_path, '') AS backdrop_path
		FROM wanted w
		LEFT JOIN public.media_items mi ON mi.content_id = w.content_id
		LEFT JOIN public.seasons s ON s.content_id = w.content_id
		LEFT JOIN public.episodes e ON e.content_id = w.content_id
		LEFT JOIN public.media_items series_mi ON series_mi.content_id = COALESCE(e.series_id, s.series_id)
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query catalog image paths: %w", err)
	}
	defer rows.Close()
	out := make(map[string]CatalogMediaImagePaths, len(ids))
	for rows.Next() {
		var item CatalogMediaImagePaths
		if err := rows.Scan(&item.MediaID, &item.PosterPath, &item.BackdropPath); err != nil {
			return nil, fmt.Errorf("scan catalog image paths: %w", err)
		}
		out[item.MediaID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
