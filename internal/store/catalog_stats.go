package store

import (
	"context"
	"fmt"
)

// CatalogStats returns aggregate counts (totals by media type and by
// library, plus 4K/HDR/Dolby-Vision quality buckets). Reads directly
// from public.media_items / public.media_files because the host SDK's
// GetCatalogStats doesn't expose quality buckets yet.
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
