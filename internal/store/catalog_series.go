package store

import (
	"context"
	"fmt"
)

// CatalogSeriesSeasons returns all seasons of a given series, with
// per-season poster URLs resolved from the provider image cache.
func (s *Store) CatalogSeriesSeasons(ctx context.Context, seriesID string, libraryIDs []string) ([]CatalogSeason, error) {
	ids := cleanIDs(libraryIDs)
	rows, err := s.pool.Query(ctx, `
		SELECT s.content_id, s.series_id, COALESCE(s.season_number, 0), COALESCE(s.title, ''),
		       COALESCE(s.overview, ''), COALESCE(s.air_date::text, ''),
		       COUNT(e.content_id)::int, COALESCE(mi.tmdb_id, ''), COALESCE(mi.tvdb_id, '')
		FROM public.seasons s
		LEFT JOIN public.episodes e ON e.season_id = s.content_id
		LEFT JOIN public.media_items mi ON mi.content_id = s.series_id
		WHERE s.series_id = $1
		  AND (cardinality($2::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = s.series_id AND mil.media_folder_id = ANY($2::int[])
		  ))
		GROUP BY s.content_id, s.series_id, s.season_number, s.title, s.overview, s.air_date, mi.tmdb_id, mi.tvdb_id
		ORDER BY s.season_number
	`, seriesID, ids)
	if err != nil {
		return nil, fmt.Errorf("query seasons: %w", err)
	}
	defer rows.Close()
	var out []CatalogSeason
	for rows.Next() {
		var season CatalogSeason
		if err := rows.Scan(&season.ContentID, &season.SeriesID, &season.SeasonNumber, &season.Title,
			&season.Overview, &season.AirDate, &season.EpisodeCount, &season.tmdbID, &season.tvdbID); err != nil {
			return nil, fmt.Errorf("scan season: %w", err)
		}
		if season.Title == "" {
			season.Title = fmt.Sprintf("Season %d", season.SeasonNumber)
		}
		season.PosterURL = s.ProviderImageURL(ctx, "series", season.tmdbID, season.tvdbID, "poster", season.SeasonNumber)
		out = append(out, season)
	}
	return out, rows.Err()
}

// CatalogSeasonEpisodes returns ordered episodes of a single season.
func (s *Store) CatalogSeasonEpisodes(ctx context.Context, seriesID string, seasonNumber int, libraryIDs []string) ([]CatalogEpisode, error) {
	ids := cleanIDs(libraryIDs)
	rows, err := s.pool.Query(ctx, `
		SELECT e.content_id, e.series_id, COALESCE(e.season_id, ''), COALESCE(e.season_number, 0),
		       COALESCE(e.episode_number, 0), COALESCE(e.title, ''), COALESCE(e.overview, ''),
		       COALESCE(e.air_date::text, ''), COALESCE(e.runtime, 0),
		       COALESCE(e.rating_imdb, 0), COALESCE(e.rating_tmdb, 0)
		FROM public.episodes e
		WHERE e.series_id = $1 AND e.season_number = $2
		  AND (cardinality($3::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = e.series_id AND mil.media_folder_id = ANY($3::int[])
		  ))
		ORDER BY e.episode_number, e.air_date
	`, seriesID, seasonNumber, ids)
	if err != nil {
		return nil, fmt.Errorf("query episodes: %w", err)
	}
	defer rows.Close()
	var out []CatalogEpisode
	for rows.Next() {
		var ep CatalogEpisode
		if err := rows.Scan(&ep.ContentID, &ep.SeriesID, &ep.SeasonID, &ep.SeasonNumber, &ep.EpisodeNumber,
			&ep.Title, &ep.Overview, &ep.AirDate, &ep.Runtime, &ep.RatingIMDB, &ep.RatingTMDB); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		if ep.Title == "" {
			ep.Title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}
