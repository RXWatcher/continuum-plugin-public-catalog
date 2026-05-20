// Package store is the plugin's Postgres access layer. Files in the package
// each own one bounded concern (config, catalog_stats, catalog_media,
// catalog_filters, catalog_item, catalog_series, catalog_quality,
// catalog_image, saved_links) so handlers don't see a single 1200-line
// surface.
//
// The plugin reads its own private tables (app_config, saved_catalog_links,
// provider_image_cache) via the schema set on the DSN's search_path. It
// ALSO reaches into the host's public.media_* schema for catalog browsing —
// the SDK's ListLibraryMedia call doesn't yet expose episode-level browsing
// or detailed quality counts. The cross-schema reach is centralised in the
// catalog_*.go files and the required grants are documented in the README.
package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by lookups that found no matching row.
var ErrNotFound = errors.New("not found")

// Store wraps the connection pool. Construct with New.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool. Most callers should use the typed
// methods on Store instead.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

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
