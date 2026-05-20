package store

import (
	"context"
	"fmt"
	"strings"
)

// CatalogMedia returns one filtered, sorted, paginated page of catalog
// items. When the requested mediaTypes is exactly ["episode"], the
// query joins through public.episodes for episode-level browsing.
func (s *Store) CatalogMedia(ctx context.Context, q CatalogMediaQuery) (*CatalogMediaResponse, error) {
	ids := cleanIDs(q.LibraryIDs)
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 48
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := 0
	if q.PageToken != "" {
		_, _ = fmt.Sscanf(q.PageToken, "offset:%d", &offset)
	}
	orderCol := "mi.title"
	switch q.Sort {
	case "year":
		orderCol = "mi.year"
	case "added_at":
		orderCol = "mi.added_at"
	case "rating":
		orderCol = "mi.rating"
	}
	dir := "ASC"
	if q.Descending {
		dir = "DESC"
	}
	mediaTypes := cleanStrings(q.MediaTypes)
	search := strings.TrimSpace(q.Query)
	genre := strings.TrimSpace(q.Genre)
	if len(mediaTypes) == 1 && mediaTypes[0] == "episode" {
		return s.catalogEpisodeMedia(ctx, ids, search, genre, q.YearMin, q.YearMax, q.Sort, dir, pageSize, offset)
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		WITH filtered AS (
			SELECT mi.content_id, mi.type, mi.title, COALESCE(mi.year, 0) AS year,
			       COALESCE(mi.overview, '') AS overview, COALESCE(mi.genres, '{}'::text[]) AS genres,
			       COALESCE(mi.runtime, 0) AS runtime, COALESCE(mi.rating_tmdb, 0) AS rating,
			       COALESCE(mi.content_rating, '') AS content_rating, COALESCE(mi.matched_at::text, '') AS added_at,
			       COALESCE(MIN(mil.media_folder_id)::text, '') AS library_id,
			       COALESCE(mi.tmdb_id, '') AS tmdb_id, COALESCE(mi.tvdb_id, '') AS tvdb_id,
			       ''::text AS series_title, 0::int AS season_number, 0::int AS episode_number
			FROM public.media_items mi
			LEFT JOIN public.media_item_libraries mil ON mil.content_id = mi.content_id
			WHERE mi.status = 'matched'
			  AND (cardinality($1::int[]) = 0 OR mil.media_folder_id = ANY($1::int[]))
			  AND (cardinality($2::text[]) = 0 OR mi.type = ANY($2::text[]))
			  AND ($3 = '' OR mi.title ILIKE '%%' || $3 || '%%' OR mi.original_title ILIKE '%%' || $3 || '%%')
			  AND ($4 = '' OR EXISTS (SELECT 1 FROM unnest(mi.genres) g WHERE g ILIKE $4))
			  AND ($5 = 0 OR mi.year >= $5)
			  AND ($6 = 0 OR mi.year <= $6)
			GROUP BY mi.content_id, mi.type, mi.title, mi.year, mi.overview, mi.genres, mi.runtime, mi.rating_tmdb, mi.content_rating, mi.matched_at, mi.tmdb_id, mi.tvdb_id
		)
		SELECT *, COUNT(*) OVER()::int
		FROM filtered mi
		ORDER BY %s %s NULLS LAST, mi.title
		LIMIT $7 OFFSET $8
	`, orderCol, dir), ids, mediaTypes, search, genre, q.YearMin, q.YearMax, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("query catalog media: %w", err)
	}
	defer rows.Close()
	resp := &CatalogMediaResponse{}
	for rows.Next() {
		var item CatalogMediaItem
		if err := rows.Scan(&item.MediaID, &item.MediaType, &item.Title, &item.Year, &item.Overview, &item.Genres, &item.RuntimeMinutes, &item.Rating, &item.ContentRating, &item.AddedAt, &item.LibraryID, &item.tmdbID, &item.tvdbID, &item.SeriesTitle, &item.SeasonNumber, &item.EpisodeNumber, &resp.TotalCount); err != nil {
			return nil, fmt.Errorf("scan catalog media: %w", err)
		}
		item.PosterURL = s.ProviderImageURL(ctx, item.MediaType, item.tmdbID, item.tvdbID, "poster", 0)
		item.BackdropURL = s.ProviderImageURL(ctx, item.MediaType, item.tmdbID, item.tvdbID, "backdrop", 0)
		resp.Items = append(resp.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if offset+len(resp.Items) < resp.TotalCount {
		resp.NextPageToken = fmt.Sprintf("offset:%d", offset+len(resp.Items))
	}
	return resp, nil
}

func (s *Store) catalogEpisodeMedia(ctx context.Context, libraryIDs []int, search, genre string, yearMin, yearMax int, sort, dir string, pageSize, offset int) (*CatalogMediaResponse, error) {
	orderCol := "ce.title"
	switch sort {
	case "year":
		orderCol = "ce.year"
	case "added_at":
		orderCol = "ce.added_at"
	case "rating":
		orderCol = "ce.rating"
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		WITH filtered AS (
			SELECT e.content_id,
			       'episode'::text AS media_type,
			       COALESCE(NULLIF(e.title, ''), 'Episode ' || e.episode_number::text) AS title,
			       COALESCE(mi.title, '') AS series_title,
			       COALESCE(e.season_number, 0) AS season_number,
			       COALESCE(e.episode_number, 0) AS episode_number,
			       COALESCE(EXTRACT(YEAR FROM e.air_date)::int, 0) AS year,
			       COALESCE(e.overview, '') AS overview,
			       COALESCE(mi.genres, '{}'::text[]) AS genres,
			       COALESCE(e.runtime, 0) AS runtime,
			       COALESCE(e.rating_tmdb, 0) AS rating,
			       COALESCE(mi.content_rating, '') AS content_rating,
			       COALESCE(MAX(mf.created_at)::text, COALESCE(e.air_date::text, '')) AS added_at,
			       COALESCE(MIN(el.media_folder_id)::text, '') AS library_id,
			       COALESCE(mi.tmdb_id, '') AS tmdb_id,
			       COALESCE(mi.tvdb_id, '') AS tvdb_id
			FROM public.episodes e
			JOIN public.media_items mi ON mi.content_id = e.series_id AND mi.status = 'matched'
			LEFT JOIN public.episode_libraries el ON el.episode_id = e.content_id
			LEFT JOIN public.media_files mf ON mf.episode_id = e.content_id AND mf.missing_since IS NULL
			WHERE (cardinality($1::int[]) = 0 OR el.media_folder_id = ANY($1::int[]))
			  AND ($2 = '' OR e.title ILIKE '%%' || $2 || '%%' OR mi.title ILIKE '%%' || $2 || '%%')
			  AND ($3 = '' OR EXISTS (SELECT 1 FROM unnest(mi.genres) g WHERE g ILIKE $3))
			  AND ($4 = 0 OR COALESCE(EXTRACT(YEAR FROM e.air_date)::int, 0) >= $4)
			  AND ($5 = 0 OR COALESCE(EXTRACT(YEAR FROM e.air_date)::int, 0) <= $5)
			GROUP BY e.content_id, e.title, e.season_number, e.episode_number, e.air_date, e.overview, e.runtime, e.rating_tmdb, mi.title, mi.genres, mi.content_rating, mi.tmdb_id, mi.tvdb_id
		)
		SELECT ce.content_id, ce.media_type, ce.title, ce.year, ce.overview, ce.genres,
		       ce.runtime, ce.rating, ce.content_rating, ce.added_at, ce.library_id,
		       ce.tmdb_id, ce.tvdb_id, ce.series_title, ce.season_number, ce.episode_number,
		       COUNT(*) OVER()::int
		FROM filtered ce
		ORDER BY %s %s NULLS LAST, ce.series_title, ce.season_number, ce.episode_number, ce.title
		LIMIT $6 OFFSET $7
	`, orderCol, dir), libraryIDs, search, genre, yearMin, yearMax, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("query episode catalog media: %w", err)
	}
	defer rows.Close()
	resp := &CatalogMediaResponse{}
	for rows.Next() {
		var item CatalogMediaItem
		if err := rows.Scan(&item.MediaID, &item.MediaType, &item.Title, &item.Year, &item.Overview, &item.Genres, &item.RuntimeMinutes, &item.Rating, &item.ContentRating, &item.AddedAt, &item.LibraryID, &item.tmdbID, &item.tvdbID, &item.SeriesTitle, &item.SeasonNumber, &item.EpisodeNumber, &resp.TotalCount); err != nil {
			return nil, fmt.Errorf("scan episode catalog media: %w", err)
		}
		item.PosterURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "poster", item.SeasonNumber)
		item.BackdropURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "backdrop", 0)
		resp.Items = append(resp.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if offset+len(resp.Items) < resp.TotalCount {
		resp.NextPageToken = fmt.Sprintf("offset:%d", offset+len(resp.Items))
	}
	return resp, nil
}
