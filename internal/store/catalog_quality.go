package store

import (
	"context"
	"fmt"
)

// CatalogItemLibraries lists the libraries (media folders) that hold an
// item, scoped to the optionally-allowed library set.
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

// CatalogItemQualities returns the available quality buckets (resolution,
// codecs, HDR) for a movie-or-series content id.
func (s *Store) CatalogItemQualities(ctx context.Context, contentID string, libraryIDs []int) ([]CatalogQuality, error) {
	return s.queryQualities(ctx, `
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
}

// CatalogSeasonQualities aggregates qualities across all episodes of
// one season.
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
	return scanQualities(rows, "season")
}

// CatalogEpisodeQualities returns the quality buckets for a single
// episode (either by episode_id or content_id since pre-refactor data
// uses both columns).
func (s *Store) CatalogEpisodeQualities(ctx context.Context, episodeID string, libraryIDs []int) ([]CatalogQuality, error) {
	return s.queryQualities(ctx, `
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
}

func (s *Store) queryQualities(ctx context.Context, sql, id string, libraryIDs []int) ([]CatalogQuality, error) {
	rows, err := s.pool.Query(ctx, sql, id, libraryIDs)
	if err != nil {
		return nil, fmt.Errorf("query qualities: %w", err)
	}
	return scanQualities(rows, "item")
}

type qualityRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

func scanQualities(rows qualityRows, label string) ([]CatalogQuality, error) {
	defer rows.Close()
	var out []CatalogQuality
	for rows.Next() {
		var q CatalogQuality
		if err := rows.Scan(&q.Resolution, &q.VideoCodec, &q.AudioCodec, &q.Container, &q.HDR, &q.Count); err != nil {
			return nil, fmt.Errorf("scan %s quality: %w", label, err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
