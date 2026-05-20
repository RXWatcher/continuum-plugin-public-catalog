package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CatalogItemDetail resolves a single content ID to its full detail
// payload. The ID may identify a top-level media item, a season, or an
// episode; the lookup walks through the three tables in order.
func (s *Store) CatalogItemDetail(ctx context.Context, contentID string, libraryIDs []string) (*CatalogItemDetail, error) {
	ids := cleanIDs(libraryIDs)
	item, err := s.catalogRootItemDetail(ctx, contentID, ids)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get catalog item: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = s.catalogSeasonDetail(ctx, contentID, ids)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get catalog season: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = s.catalogEpisodeDetail(ctx, contentID, ids)
	}
	if err != nil {
		return nil, fmt.Errorf("get catalog item: %w", err)
	}
	switch item.Type {
	case "season":
		item.Qualities, err = s.CatalogSeasonQualities(ctx, item.SeriesID, item.SeasonNumber, ids)
	case "episode":
		item.Qualities, err = s.CatalogEpisodeQualities(ctx, item.ContentID, ids)
	default:
		item.Qualities, err = s.CatalogItemQualities(ctx, item.ContentID, ids)
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) catalogRootItemDetail(ctx context.Context, contentID string, ids []int) (*CatalogItemDetail, error) {
	var item CatalogItemDetail
	err := s.pool.QueryRow(ctx, `
		SELECT mi.content_id, mi.type, mi.title, COALESCE(mi.original_title, ''),
		       COALESCE(mi.year, 0), COALESCE(mi.overview, ''), COALESCE(mi.tagline, ''),
		       COALESCE(mi.runtime, 0), COALESCE(mi.content_rating, ''), COALESCE(mi.genres, '{}'::text[]),
		       COALESCE(mi.studios, '{}'::text[]), COALESCE(mi.networks, '{}'::text[]), COALESCE(mi.countries, '{}'::text[]),
		       COALESCE(mi.first_air_date, ''), COALESCE(mi.last_air_date, ''), COALESCE(mi.release_date::text, ''),
		       COALESCE(mi.rating_imdb, 0), COALESCE(mi.rating_tmdb, 0),
		       COALESCE(mi.rating_rt_critic, 0), COALESCE(mi.rating_rt_audience, 0),
		       COALESCE(mi.logo_path, ''), COALESCE(mi.season_count, 0),
		       COALESCE(mi.tmdb_id, ''), COALESCE(mi.tvdb_id, '')
		FROM public.media_items mi
		WHERE mi.content_id = $1
		  AND mi.status = 'matched'
		  AND (cardinality($2::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = mi.content_id AND mil.media_folder_id = ANY($2::int[])
		  ))
	`, contentID, ids).Scan(
		&item.ContentID, &item.Type, &item.Title, &item.OriginalTitle, &item.Year, &item.Overview, &item.Tagline,
		&item.Runtime, &item.ContentRating, &item.Genres, &item.Studios, &item.Networks, &item.Countries,
		&item.FirstAirDate, &item.LastAirDate, &item.ReleaseDate, &item.RatingIMDB, &item.RatingTMDB,
		&item.RatingRTCritic, &item.RatingRTAudience, &item.LogoURL, &item.SeasonCount, &item.tmdbID, &item.tvdbID,
	)
	if err != nil {
		return nil, err
	}
	libs, err := s.CatalogItemLibraries(ctx, contentID, ids)
	if err != nil {
		return nil, err
	}
	item.Libraries = libs
	item.PosterURL = s.ProviderImageURL(ctx, item.Type, item.tmdbID, item.tvdbID, "poster", 0)
	item.BackdropURL = s.ProviderImageURL(ctx, item.Type, item.tmdbID, item.tvdbID, "backdrop", 0)
	return &item, nil
}

func (s *Store) catalogSeasonDetail(ctx context.Context, contentID string, ids []int) (*CatalogItemDetail, error) {
	var item CatalogItemDetail
	err := s.pool.QueryRow(ctx, `
		SELECT s.content_id,
		       'season'::text,
		       COALESCE(NULLIF(s.title, ''), CASE WHEN s.season_number = 0 THEN 'Specials' ELSE 'Season ' || s.season_number::text END),
		       ''::text,
		       COALESCE(mi.content_id, ''),
		       COALESCE(mi.title, ''),
		       COALESCE(s.season_number, 0),
		       0::int,
		       COALESCE(COUNT(e.content_id), 0)::int,
		       COALESCE(EXTRACT(YEAR FROM s.air_date)::int, 0),
		       COALESCE(s.overview, ''),
		       ''::text,
		       0::int,
		       COALESCE(mi.content_rating, ''),
		       COALESCE(mi.genres, '{}'::text[]),
		       COALESCE(mi.studios, '{}'::text[]),
		       COALESCE(mi.networks, '{}'::text[]),
		       COALESCE(mi.countries, '{}'::text[]),
		       COALESCE(mi.first_air_date, ''),
		       COALESCE(mi.last_air_date, ''),
		       ''::text,
		       0::float8,
		       0::float8,
		       0::int,
		       0::int,
		       COALESCE(mi.logo_path, ''),
		       COALESCE(mi.season_count, 0),
		       COALESCE(s.air_date::text, ''),
		       (COALESCE(s.season_number, 0) = 0),
		       COALESCE(mi.tmdb_id, ''),
		       COALESCE(mi.tvdb_id, '')
		FROM public.seasons s
		JOIN public.media_items mi ON mi.content_id = s.series_id AND mi.status = 'matched'
		LEFT JOIN public.episodes e ON e.season_id = s.content_id
		WHERE s.content_id = $1
		  AND (cardinality($2::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = s.series_id AND mil.media_folder_id = ANY($2::int[])
		  ))
		GROUP BY s.content_id, s.title, s.season_number, s.air_date, s.overview,
		         mi.content_id, mi.title, mi.content_rating, mi.genres, mi.studios, mi.networks,
		         mi.countries, mi.first_air_date, mi.last_air_date, mi.logo_path, mi.season_count,
		         mi.tmdb_id, mi.tvdb_id
	`, contentID, ids).Scan(
		&item.ContentID, &item.Type, &item.Title, &item.OriginalTitle, &item.SeriesID, &item.SeriesTitle,
		&item.SeasonNumber, &item.EpisodeNumber, &item.EpisodeCount, &item.Year, &item.Overview,
		&item.Tagline, &item.Runtime, &item.ContentRating, &item.Genres, &item.Studios, &item.Networks,
		&item.Countries, &item.FirstAirDate, &item.LastAirDate, &item.ReleaseDate, &item.RatingIMDB,
		&item.RatingTMDB, &item.RatingRTCritic, &item.RatingRTAudience, &item.LogoURL, &item.SeasonCount,
		&item.AirDate, &item.IsSpecials, &item.tmdbID, &item.tvdbID,
	)
	if err != nil {
		return nil, err
	}
	libs, err := s.CatalogItemLibraries(ctx, item.SeriesID, ids)
	if err != nil {
		return nil, err
	}
	item.Libraries = libs
	item.PosterURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "poster", item.SeasonNumber)
	item.BackdropURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "backdrop", 0)
	return &item, nil
}

func (s *Store) catalogEpisodeDetail(ctx context.Context, contentID string, ids []int) (*CatalogItemDetail, error) {
	var item CatalogItemDetail
	err := s.pool.QueryRow(ctx, `
		SELECT e.content_id,
		       'episode'::text,
		       COALESCE(NULLIF(e.title, ''), 'Episode ' || e.episode_number::text),
		       ''::text,
		       COALESCE(mi.content_id, ''),
		       COALESCE(mi.title, ''),
		       COALESCE(e.season_number, 0),
		       COALESCE(e.episode_number, 0),
		       0::int,
		       COALESCE(EXTRACT(YEAR FROM e.air_date)::int, 0),
		       COALESCE(e.overview, ''),
		       ''::text,
		       COALESCE(e.runtime, 0),
		       COALESCE(mi.content_rating, ''),
		       COALESCE(mi.genres, '{}'::text[]),
		       COALESCE(mi.studios, '{}'::text[]),
		       COALESCE(mi.networks, '{}'::text[]),
		       COALESCE(mi.countries, '{}'::text[]),
		       COALESCE(mi.first_air_date, ''),
		       COALESCE(mi.last_air_date, ''),
		       ''::text,
		       COALESCE(e.rating_imdb, 0),
		       COALESCE(e.rating_tmdb, 0),
		       0::int,
		       0::int,
		       COALESCE(mi.logo_path, ''),
		       COALESCE(mi.season_count, 0),
		       COALESCE(e.air_date::text, ''),
		       (COALESCE(e.season_number, 0) = 0),
		       COALESCE(mi.tmdb_id, ''),
		       COALESCE(mi.tvdb_id, '')
		FROM public.episodes e
		JOIN public.media_items mi ON mi.content_id = e.series_id AND mi.status = 'matched'
		WHERE e.content_id = $1
		  AND (cardinality($2::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = e.series_id AND mil.media_folder_id = ANY($2::int[])
		  ))
	`, contentID, ids).Scan(
		&item.ContentID, &item.Type, &item.Title, &item.OriginalTitle, &item.SeriesID, &item.SeriesTitle,
		&item.SeasonNumber, &item.EpisodeNumber, &item.EpisodeCount, &item.Year, &item.Overview,
		&item.Tagline, &item.Runtime, &item.ContentRating, &item.Genres, &item.Studios, &item.Networks,
		&item.Countries, &item.FirstAirDate, &item.LastAirDate, &item.ReleaseDate, &item.RatingIMDB,
		&item.RatingTMDB, &item.RatingRTCritic, &item.RatingRTAudience, &item.LogoURL, &item.SeasonCount,
		&item.AirDate, &item.IsSpecials, &item.tmdbID, &item.tvdbID,
	)
	if err != nil {
		return nil, err
	}
	libs, err := s.CatalogItemLibraries(ctx, item.SeriesID, ids)
	if err != nil {
		return nil, err
	}
	item.Libraries = libs
	item.PosterURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "poster", item.SeasonNumber)
	item.BackdropURL = s.ProviderImageURL(ctx, "series", item.tmdbID, item.tvdbID, "backdrop", 0)
	return &item, nil
}
