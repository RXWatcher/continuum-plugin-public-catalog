package store

import (
	"context"
	"fmt"
)

// CatalogFilters returns the genre / year / decade dictionary the
// catalog UI uses to render its dropdowns. Scope is the AND-ed
// libraries+mediaTypes pair. When mediaTypes is ["episode"], the
// filters come from public.episodes.
func (s *Store) CatalogFilters(ctx context.Context, libraryIDs, mediaTypes []string) (*CatalogFilters, error) {
	ids, denyAll := s.allowedLibraryIDs(libraryIDs)
	if denyAll {
		return &CatalogFilters{}, nil
	}
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
