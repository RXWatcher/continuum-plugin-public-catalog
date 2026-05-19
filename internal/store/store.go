package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginrt "github.com/ContinuumApp/continuum-plugin-public-catalog/internal/runtime"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type CatalogStats struct {
	TotalItems      int                   `json:"totalItems"`
	MediaTypeCounts []CatalogTypeCount    `json:"mediaTypeCounts"`
	LibraryCounts   []CatalogLibraryCount `json:"libraryCounts"`
	QualityCounts   []CatalogQualityCount `json:"qualityCounts"`
}

type CatalogTypeCount struct {
	MediaType string `json:"mediaType"`
	Count     int    `json:"count"`
}

type CatalogLibraryCount struct {
	LibraryID   string `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	MediaType   string `json:"mediaType"`
	Count       int    `json:"count"`
}

type CatalogQualityCount struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type CatalogFilters struct {
	Genres  []string        `json:"genres"`
	Years   []int           `json:"years"`
	Decades []CatalogDecade `json:"decades"`
}

type CatalogDecade struct {
	Label   string `json:"label"`
	YearMin int    `json:"yearMin"`
	YearMax int    `json:"yearMax"`
}

type CatalogMediaResponse struct {
	Items         []CatalogMediaItem `json:"items"`
	NextPageToken string             `json:"nextPageToken"`
	TotalCount    int                `json:"totalCount"`
}

type CatalogMediaItem struct {
	MediaID        string   `json:"mediaId"`
	LibraryID      string   `json:"libraryId,omitempty"`
	MediaType      string   `json:"mediaType"`
	Title          string   `json:"title"`
	SeriesTitle    string   `json:"seriesTitle,omitempty"`
	SeasonNumber   int      `json:"seasonNumber,omitempty"`
	EpisodeNumber  int      `json:"episodeNumber,omitempty"`
	Year           int      `json:"year,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	PosterURL      string   `json:"posterUrl,omitempty"`
	BackdropURL    string   `json:"backdropUrl,omitempty"`
	Genres         []string `json:"genres"`
	RuntimeMinutes int      `json:"runtimeMinutes,omitempty"`
	Rating         float64  `json:"rating,omitempty"`
	ContentRating  string   `json:"contentRating,omitempty"`
	AddedAt        string   `json:"addedAt,omitempty"`
	tmdbID         string
	tvdbID         string
}

type CatalogItemDetail struct {
	ContentID        string           `json:"contentId"`
	Type             string           `json:"type"`
	Title            string           `json:"title"`
	OriginalTitle    string           `json:"originalTitle,omitempty"`
	SeriesID         string           `json:"seriesId,omitempty"`
	SeriesTitle      string           `json:"seriesTitle,omitempty"`
	SeasonNumber     int              `json:"seasonNumber,omitempty"`
	EpisodeNumber    int              `json:"episodeNumber,omitempty"`
	EpisodeCount     int              `json:"episodeCount,omitempty"`
	Year             int              `json:"year,omitempty"`
	Overview         string           `json:"overview,omitempty"`
	Tagline          string           `json:"tagline,omitempty"`
	Runtime          int              `json:"runtime,omitempty"`
	ContentRating    string           `json:"contentRating,omitempty"`
	Genres           []string         `json:"genres"`
	Studios          []string         `json:"studios,omitempty"`
	Networks         []string         `json:"networks,omitempty"`
	Countries        []string         `json:"countries,omitempty"`
	FirstAirDate     string           `json:"firstAirDate,omitempty"`
	LastAirDate      string           `json:"lastAirDate,omitempty"`
	ReleaseDate      string           `json:"releaseDate,omitempty"`
	RatingIMDB       float64          `json:"ratingImdb,omitempty"`
	RatingTMDB       float64          `json:"ratingTmdb,omitempty"`
	RatingRTCritic   int              `json:"ratingRtCritic,omitempty"`
	RatingRTAudience int              `json:"ratingRtAudience,omitempty"`
	PosterURL        string           `json:"posterUrl,omitempty"`
	BackdropURL      string           `json:"backdropUrl,omitempty"`
	LogoURL          string           `json:"logoUrl,omitempty"`
	SeasonCount      int              `json:"seasonCount,omitempty"`
	AirDate          string           `json:"airDate,omitempty"`
	IsSpecials       bool             `json:"isSpecials,omitempty"`
	Libraries        []CatalogLibrary `json:"libraries,omitempty"`
	Qualities        []CatalogQuality `json:"qualities,omitempty"`
	tmdbID           string
	tvdbID           string
}

type CatalogLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type CatalogQuality struct {
	Resolution string `json:"resolution,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	Container  string `json:"container,omitempty"`
	HDR        bool   `json:"hdr,omitempty"`
	Count      int    `json:"count"`
}

type CatalogSeason struct {
	ContentID    string `json:"contentId"`
	SeriesID     string `json:"seriesId"`
	SeasonNumber int    `json:"seasonNumber"`
	Title        string `json:"title"`
	Overview     string `json:"overview,omitempty"`
	AirDate      string `json:"airDate,omitempty"`
	PosterURL    string `json:"posterUrl,omitempty"`
	EpisodeCount int    `json:"episodeCount"`
	tmdbID       string
	tvdbID       string
}

type CatalogEpisode struct {
	ContentID     string  `json:"contentId"`
	SeriesID      string  `json:"seriesId"`
	SeasonID      string  `json:"seasonId,omitempty"`
	SeasonNumber  int     `json:"seasonNumber"`
	EpisodeNumber int     `json:"episodeNumber"`
	Title         string  `json:"title"`
	Overview      string  `json:"overview,omitempty"`
	AirDate       string  `json:"airDate,omitempty"`
	Runtime       int     `json:"runtime,omitempty"`
	RatingIMDB    float64 `json:"ratingImdb,omitempty"`
	RatingTMDB    float64 `json:"ratingTmdb,omitempty"`
	StillURL      string  `json:"stillUrl,omitempty"`
}

type SavedCatalogLink struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Token      string    `json:"token"`
	URL        string    `json:"url"`
	HTML       string    `json:"html"`
	MediaTypes []string  `json:"mediaTypes"`
	LibraryIDs []string  `json:"libraryIds"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS app_config (
  id INTEGER PRIMARY KEY DEFAULT 1,
  data JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT app_config_singleton CHECK (id = 1)
);
INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING;
CREATE TABLE IF NOT EXISTS saved_catalog_links (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  token TEXT NOT NULL,
  url TEXT NOT NULL,
  html TEXT NOT NULL DEFAULT '',
  media_types TEXT[] NOT NULL DEFAULT '{}'::text[],
  library_ids TEXT[] NOT NULL DEFAULT '{}'::text[],
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE saved_catalog_links ADD COLUMN IF NOT EXISTS html TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS provider_image_cache (
  cache_key TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL
);
`)
	return err
}

func (s *Store) GetConfig(ctx context.Context) (pluginrt.Config, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM app_config WHERE id = 1`).Scan(&raw)
	if err == pgx.ErrNoRows {
		if _, err := s.pool.Exec(ctx, `INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING`); err != nil {
			return pluginrt.Config{}, fmt.Errorf("ensure app_config: %w", err)
		}
		return s.GetConfig(ctx)
	}
	if err != nil {
		return pluginrt.Config{}, fmt.Errorf("get app_config: %w", err)
	}
	cfg := pluginrt.DefaultAppConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return pluginrt.Config{}, fmt.Errorf("decode app_config: %w", err)
		}
	}
	return pluginrt.NormalizeAppConfig(cfg)
}

func (s *Store) UpdateConfig(ctx context.Context, cfg pluginrt.Config) error {
	cfg, err := pluginrt.NormalizeAppConfig(cfg)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode app_config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_config (id, data, updated_at) VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()
	`, raw)
	if err != nil {
		return fmt.Errorf("update app_config: %w", err)
	}
	return nil
}

func (s *Store) ImportLegacyConfig(ctx context.Context, legacy pluginrt.Config) (pluginrt.Config, error) {
	current, err := s.GetConfig(ctx)
	if err != nil {
		return pluginrt.Config{}, err
	}
	defaultCfg, err := pluginrt.NormalizeAppConfig(pluginrt.DefaultAppConfig())
	if err != nil {
		return pluginrt.Config{}, err
	}
	if !reflect.DeepEqual(current, defaultCfg) {
		return current, nil
	}
	legacy.DatabaseURL = ""
	if reflect.DeepEqual(legacy, current) {
		return current, nil
	}
	if err := s.UpdateConfig(ctx, legacy); err != nil {
		return pluginrt.Config{}, err
	}
	return s.GetConfig(ctx)
}

func (s *Store) CatalogStats(ctx context.Context, libraryIDs []string) (*CatalogStats, error) {
	ids := cleanIDs(libraryIDs)
	stats := &CatalogStats{}

	typeRows, err := s.pool.Query(ctx, `
		SELECT mi.type, COUNT(DISTINCT mi.content_id)
		FROM public.media_items mi
		WHERE mi.status = 'matched'
		  AND (cardinality($1::int[]) = 0 OR EXISTS (
		    SELECT 1 FROM public.media_item_libraries mil
		    WHERE mil.content_id = mi.content_id AND mil.media_folder_id = ANY($1::int[])
		  ))
		GROUP BY mi.type
		ORDER BY mi.type
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query media type stats: %w", err)
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var c CatalogTypeCount
		if err := typeRows.Scan(&c.MediaType, &c.Count); err != nil {
			return nil, fmt.Errorf("scan media type stats: %w", err)
		}
		stats.MediaTypeCounts = append(stats.MediaTypeCounts, c)
		stats.TotalItems += c.Count
	}
	if err := typeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media type stats: %w", err)
	}

	libRows, err := s.pool.Query(ctx, `
		SELECT mf.id::text, mf.name, mf.type, COUNT(DISTINCT mi.content_id)
		FROM public.media_folders mf
		LEFT JOIN public.media_item_libraries mil ON mil.media_folder_id = mf.id
		LEFT JOIN public.media_items mi ON mi.content_id = mil.content_id AND mi.status = 'matched'
		WHERE COALESCE(mf.enabled, true)
		  AND (cardinality($1::int[]) = 0 OR mf.id = ANY($1::int[]))
		GROUP BY mf.id, mf.name, mf.type
		HAVING COUNT(DISTINCT mi.content_id) > 0
		ORDER BY mf.sort_order, mf.id
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query library stats: %w", err)
	}
	defer libRows.Close()
	for libRows.Next() {
		var c CatalogLibraryCount
		if err := libRows.Scan(&c.LibraryID, &c.LibraryName, &c.MediaType, &c.Count); err != nil {
			return nil, fmt.Errorf("scan library stats: %w", err)
		}
		stats.LibraryCounts = append(stats.LibraryCounts, c)
	}
	if err := libRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library stats: %w", err)
	}

	qualityRows, err := s.pool.Query(ctx, `
		SELECT key, label, count FROM (
			SELECT '4k'::text AS key, '4K'::text AS label, COUNT(DISTINCT mf.content_id)::int AS count, 1 AS ord
			FROM public.media_files mf
			JOIN public.media_items mi ON mi.content_id = mf.content_id AND mi.status = 'matched'
			WHERE mf.missing_since IS NULL
			  AND mf.resolution = '2160p'
			  AND (cardinality($1::int[]) = 0 OR mf.media_folder_id = ANY($1::int[]))
			UNION ALL
			SELECT '4k_hdr', '4K HDR', COUNT(DISTINCT mf.content_id)::int, 2
			FROM public.media_files mf
			JOIN public.media_items mi ON mi.content_id = mf.content_id AND mi.status = 'matched'
			WHERE mf.missing_since IS NULL
			  AND mf.resolution = '2160p'
			  AND COALESCE(mf.hdr, false)
			  AND (cardinality($1::int[]) = 0 OR mf.media_folder_id = ANY($1::int[]))
			UNION ALL
			SELECT '4k_dv', '4K Dolby Vision', COUNT(DISTINCT mf.content_id)::int, 3
			FROM public.media_files mf
			JOIN public.media_items mi ON mi.content_id = mf.content_id AND mi.status = 'matched'
			WHERE mf.missing_since IS NULL
			  AND mf.resolution = '2160p'
			  AND (mf.video_tracks::text ILIKE '%dolby%vision%' OR mf.video_tracks::text ILIKE '%dovi%')
			  AND (cardinality($1::int[]) = 0 OR mf.media_folder_id = ANY($1::int[]))
		) q
		ORDER BY ord
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query quality stats: %w", err)
	}
	defer qualityRows.Close()
	for qualityRows.Next() {
		var c CatalogQualityCount
		if err := qualityRows.Scan(&c.Key, &c.Label, &c.Count); err != nil {
			return nil, fmt.Errorf("scan quality stats: %w", err)
		}
		stats.QualityCounts = append(stats.QualityCounts, c)
	}
	if err := qualityRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quality stats: %w", err)
	}

	return stats, nil
}

type CatalogMediaQuery struct {
	LibraryIDs []string
	MediaTypes []string
	Query      string
	Genre      string
	YearMin    int
	YearMax    int
	Sort       string
	Descending bool
	PageSize   int
	PageToken  string
}

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

func (s *Store) CatalogFilters(ctx context.Context, libraryIDs, mediaTypes []string) (*CatalogFilters, error) {
	ids := cleanIDs(libraryIDs)
	types := cleanStrings(mediaTypes)
	if len(types) == 1 && types[0] == "episode" {
		return s.catalogEpisodeFilters(ctx, ids)
	}
	out := &CatalogFilters{}

	genreRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT g
		FROM public.media_items mi
		JOIN public.media_item_libraries mil ON mil.content_id = mi.content_id
		CROSS JOIN LATERAL unnest(COALESCE(mi.genres, '{}'::text[])) AS g
		WHERE mi.status = 'matched'
		  AND g <> ''
		  AND (cardinality($1::int[]) = 0 OR mil.media_folder_id = ANY($1::int[]))
		  AND (cardinality($2::text[]) = 0 OR mi.type = ANY($2::text[]))
		ORDER BY g
	`, ids, types)
	if err != nil {
		return nil, fmt.Errorf("query catalog genres: %w", err)
	}
	defer genreRows.Close()
	for genreRows.Next() {
		var genre string
		if err := genreRows.Scan(&genre); err != nil {
			return nil, fmt.Errorf("scan catalog genre: %w", err)
		}
		out.Genres = append(out.Genres, genre)
	}
	if err := genreRows.Err(); err != nil {
		return nil, err
	}

	yearRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT mi.year::int
		FROM public.media_items mi
		JOIN public.media_item_libraries mil ON mil.content_id = mi.content_id
		WHERE mi.status = 'matched'
		  AND mi.year IS NOT NULL
		  AND mi.year > 0
		  AND (cardinality($1::int[]) = 0 OR mil.media_folder_id = ANY($1::int[]))
		  AND (cardinality($2::text[]) = 0 OR mi.type = ANY($2::text[]))
		ORDER BY mi.year DESC
	`, ids, types)
	if err != nil {
		return nil, fmt.Errorf("query catalog years: %w", err)
	}
	defer yearRows.Close()
	decadeSeen := map[int]bool{}
	for yearRows.Next() {
		var year int
		if err := yearRows.Scan(&year); err != nil {
			return nil, fmt.Errorf("scan catalog year: %w", err)
		}
		out.Years = append(out.Years, year)
		decade := (year / 10) * 10
		if !decadeSeen[decade] {
			decadeSeen[decade] = true
			out.Decades = append(out.Decades, CatalogDecade{
				Label:   fmt.Sprintf("%ds", decade),
				YearMin: decade,
				YearMax: decade + 9,
			})
		}
	}
	if err := yearRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) catalogEpisodeFilters(ctx context.Context, libraryIDs []int) (*CatalogFilters, error) {
	out := &CatalogFilters{}
	genreRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT g
		FROM public.episodes e
		JOIN public.media_items mi ON mi.content_id = e.series_id AND mi.status = 'matched'
		JOIN public.episode_libraries el ON el.episode_id = e.content_id
		CROSS JOIN LATERAL unnest(COALESCE(mi.genres, '{}'::text[])) AS g
		WHERE g <> ''
		  AND (cardinality($1::int[]) = 0 OR el.media_folder_id = ANY($1::int[]))
		ORDER BY g
	`, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query episode genres: %w", err)
	}
	defer genreRows.Close()
	for genreRows.Next() {
		var genre string
		if err := genreRows.Scan(&genre); err != nil {
			return nil, fmt.Errorf("scan episode genre: %w", err)
		}
		out.Genres = append(out.Genres, genre)
	}
	if err := genreRows.Err(); err != nil {
		return nil, err
	}

	yearRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT EXTRACT(YEAR FROM e.air_date)::int AS year
		FROM public.episodes e
		JOIN public.episode_libraries el ON el.episode_id = e.content_id
		WHERE e.air_date IS NOT NULL
		  AND (cardinality($1::int[]) = 0 OR el.media_folder_id = ANY($1::int[]))
		ORDER BY year DESC
	`, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query episode years: %w", err)
	}
	defer yearRows.Close()
	decadeSeen := map[int]bool{}
	for yearRows.Next() {
		var year int
		if err := yearRows.Scan(&year); err != nil {
			return nil, fmt.Errorf("scan episode year: %w", err)
		}
		if year <= 0 {
			continue
		}
		out.Years = append(out.Years, year)
		decade := (year / 10) * 10
		if !decadeSeen[decade] {
			decadeSeen[decade] = true
			out.Decades = append(out.Decades, CatalogDecade{
				Label:   fmt.Sprintf("%ds", decade),
				YearMin: decade,
				YearMax: decade + 9,
			})
		}
	}
	if err := yearRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ProviderImageURL(ctx context.Context, itemType, tmdbID, tvdbID, imageKind string, seasonNumber int) string {
	cacheKey := strings.Join([]string{itemType, tmdbID, tvdbID, imageKind, fmt.Sprintf("%d", seasonNumber)}, ":")
	return s.cachedProviderImage(ctx, cacheKey)
}

func (s *Store) cachedProviderImage(ctx context.Context, cacheKey string) string {
	var out string
	if err := s.pool.QueryRow(ctx, `SELECT url FROM provider_image_cache WHERE cache_key = $1 AND expires_at > NOW()`, cacheKey).Scan(&out); err != nil {
		return ""
	}
	return out
}

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

func (s *Store) CatalogItemLibraries(ctx context.Context, contentID string, ids []int) ([]CatalogLibrary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT mf.id::text, mf.name, mf.type
		FROM public.media_item_libraries mil
		JOIN public.media_folders mf ON mf.id = mil.media_folder_id
		WHERE mil.content_id = $1
		  AND COALESCE(mf.enabled, true)
		  AND (cardinality($2::int[]) = 0 OR mf.id = ANY($2::int[]))
		ORDER BY mf.sort_order, mf.id
	`, contentID, ids)
	if err != nil {
		return nil, fmt.Errorf("query item libraries: %w", err)
	}
	defer rows.Close()
	var out []CatalogLibrary
	for rows.Next() {
		var lib CatalogLibrary
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Type); err != nil {
			return nil, fmt.Errorf("scan item library: %w", err)
		}
		out = append(out, lib)
	}
	return out, rows.Err()
}

func (s *Store) CatalogItemQualities(ctx context.Context, contentID string, libraryIDs []int) ([]CatalogQuality, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(resolution, ''), COALESCE(codec_video, ''), COALESCE(codec_audio, ''),
		       COALESCE(container, ''), COALESCE(hdr, false), COUNT(*)::int
		FROM public.media_files
		WHERE content_id = $1
		  AND missing_since IS NULL
		  AND (cardinality($2::int[]) = 0 OR media_folder_id = ANY($2::int[]))
		GROUP BY resolution, codec_video, codec_audio, container, hdr
		ORDER BY resolution DESC NULLS LAST, hdr DESC, codec_video, codec_audio
		LIMIT 12
	`, contentID, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query item qualities: %w", err)
	}
	defer rows.Close()
	var out []CatalogQuality
	for rows.Next() {
		var q CatalogQuality
		if err := rows.Scan(&q.Resolution, &q.VideoCodec, &q.AudioCodec, &q.Container, &q.HDR, &q.Count); err != nil {
			return nil, fmt.Errorf("scan item quality: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CatalogSeasonQualities(ctx context.Context, seriesID string, seasonNumber int, libraryIDs []int) ([]CatalogQuality, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(mf.resolution, ''), COALESCE(mf.codec_video, ''), COALESCE(mf.codec_audio, ''),
		       COALESCE(mf.container, ''), COALESCE(mf.hdr, false), COUNT(*)::int
		FROM public.media_files mf
		JOIN public.episodes e ON e.content_id = mf.episode_id
		WHERE e.series_id = $1
		  AND e.season_number = $2
		  AND mf.missing_since IS NULL
		  AND (cardinality($3::int[]) = 0 OR mf.media_folder_id = ANY($3::int[]))
		GROUP BY mf.resolution, mf.codec_video, mf.codec_audio, mf.container, mf.hdr
		ORDER BY mf.resolution DESC NULLS LAST, mf.hdr DESC, mf.codec_video, mf.codec_audio
		LIMIT 12
	`, seriesID, seasonNumber, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query season qualities: %w", err)
	}
	defer rows.Close()
	var out []CatalogQuality
	for rows.Next() {
		var q CatalogQuality
		if err := rows.Scan(&q.Resolution, &q.VideoCodec, &q.AudioCodec, &q.Container, &q.HDR, &q.Count); err != nil {
			return nil, fmt.Errorf("scan season quality: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CatalogEpisodeQualities(ctx context.Context, episodeID string, libraryIDs []int) ([]CatalogQuality, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(resolution, ''), COALESCE(codec_video, ''), COALESCE(codec_audio, ''),
		       COALESCE(container, ''), COALESCE(hdr, false), COUNT(*)::int
		FROM public.media_files
		WHERE (episode_id = $1 OR content_id = $1)
		  AND missing_since IS NULL
		  AND (cardinality($2::int[]) = 0 OR media_folder_id = ANY($2::int[]))
		GROUP BY resolution, codec_video, codec_audio, container, hdr
		ORDER BY resolution DESC NULLS LAST, hdr DESC, codec_video, codec_audio
		LIMIT 12
	`, episodeID, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query episode qualities: %w", err)
	}
	defer rows.Close()
	var out []CatalogQuality
	for rows.Next() {
		var q CatalogQuality
		if err := rows.Scan(&q.Resolution, &q.VideoCodec, &q.AudioCodec, &q.Container, &q.HDR, &q.Count); err != nil {
			return nil, fmt.Errorf("scan episode quality: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

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
		if err := rows.Scan(&season.ContentID, &season.SeriesID, &season.SeasonNumber, &season.Title, &season.Overview, &season.AirDate, &season.EpisodeCount, &season.tmdbID, &season.tvdbID); err != nil {
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
		if err := rows.Scan(&ep.ContentID, &ep.SeriesID, &ep.SeasonID, &ep.SeasonNumber, &ep.EpisodeNumber, &ep.Title, &ep.Overview, &ep.AirDate, &ep.Runtime, &ep.RatingIMDB, &ep.RatingTMDB); err != nil {
			return nil, fmt.Errorf("scan episode: %w", err)
		}
		if ep.Title == "" {
			ep.Title = fmt.Sprintf("Episode %d", ep.EpisodeNumber)
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *Store) SaveCatalogLink(ctx context.Context, link SavedCatalogLink) (SavedCatalogLink, error) {
	link.Name = strings.TrimSpace(link.Name)
	if link.Name == "" {
		return SavedCatalogLink{}, fmt.Errorf("link name is required")
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO saved_catalog_links (name, token, url, html, media_types, library_ids)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET
		  token = EXCLUDED.token,
		  url = EXCLUDED.url,
		  html = EXCLUDED.html,
		  media_types = EXCLUDED.media_types,
		  library_ids = EXCLUDED.library_ids,
		  updated_at = NOW()
		RETURNING id, name, token, url, html, media_types, library_ids, created_at, updated_at
	`, link.Name, link.Token, link.URL, link.HTML, link.MediaTypes, link.LibraryIDs).Scan(
		&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt,
	)
	if err != nil {
		return SavedCatalogLink{}, fmt.Errorf("save catalog link: %w", err)
	}
	return link, nil
}

func (s *Store) ListCatalogLinks(ctx context.Context) ([]SavedCatalogLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, token, url, html, media_types, library_ids, created_at, updated_at
		FROM saved_catalog_links
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list catalog links: %w", err)
	}
	defer rows.Close()
	var out []SavedCatalogLink
	for rows.Next() {
		var link SavedCatalogLink
		if err := rows.Scan(&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan catalog link: %w", err)
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog links: %w", err)
	}
	return out, nil
}

func (s *Store) GetCatalogLinkByName(ctx context.Context, name string) (SavedCatalogLink, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SavedCatalogLink{}, false, nil
	}
	var link SavedCatalogLink
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, token, url, html, media_types, library_ids, created_at, updated_at
		FROM saved_catalog_links
		WHERE lower(name) = lower($1)
	`, name).Scan(&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt)
	if err == pgx.ErrNoRows {
		return SavedCatalogLink{}, false, nil
	}
	if err != nil {
		return SavedCatalogLink{}, false, fmt.Errorf("get catalog link: %w", err)
	}
	return link, true, nil
}

func (s *Store) DeleteCatalogLink(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM saved_catalog_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete catalog link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func cleanIDs(in []string) []int {
	out := []int{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(raw, "%d", &id); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func cleanStrings(in []string) []string {
	out := []string{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			out = append(out, raw)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
