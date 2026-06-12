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
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by lookups that found no matching row.
var ErrNotFound = errors.New("not found")

// Store wraps the connection pool. Construct with New.
type Store struct {
	pool *pgxpool.Pool

	// publicMu guards publicLibraryIDs, which is reloaded whenever config
	// changes (Bootstrap / UpdateConfig). It is the hard default-deny
	// allowlist of host library ids the public catalog may EVER expose.
	publicMu         sync.RWMutex
	publicLibraryIDs []int
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool exposes the underlying pool. Most callers should use the typed
// methods on Store instead.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// SetPublicLibraryIDs installs the operator-curated allowlist of host
// library ids that may be exposed publicly. Called on bootstrap and on
// every config update so the scoping floor tracks live config. An empty
// list means "expose nothing".
func (s *Store) SetPublicLibraryIDs(ids []string) {
	parsed := cleanIDs(ids)
	s.publicMu.Lock()
	s.publicLibraryIDs = parsed
	s.publicMu.Unlock()
}

// ScopePublicLibraryIDs intersects a requested/token library scope with
// the public allowlist floor and returns the result as string ids plus a
// denyAll flag. Server handlers that bypass the store (e.g. host-SDK
// ListLibraryMedia for ebooks/audiobooks or image enrichment) MUST floor
// their library scope through this before calling out, so the allowlist
// is enforced on every path, not just direct DB queries.
func (s *Store) ScopePublicLibraryIDs(requested []string) (ids []string, denyAll bool) {
	intIDs, deny := s.allowedLibraryIDs(requested)
	if deny {
		return nil, true
	}
	out := make([]string, 0, len(intIDs))
	for _, id := range intIDs {
		out = append(out, strconv.Itoa(id))
	}
	return out, false
}

// allowedLibraryIDs returns the FINAL, hard-floored set of library ids a
// query may touch, plus a denyAll flag. It intersects the public
// allowlist (default-deny floor) with the optional per-request/token
// scope. Rules:
//
//   - Empty allowlist  -> denyAll = true (expose nothing, ever).
//   - Empty request    -> the full allowlist.
//   - Non-empty request-> allowlist ∩ request; if the intersection is
//     empty the request asked only for forbidden libraries, so denyAll.
//
// Every catalog store query MUST route its library scoping through this
// helper so the floor can't be forgotten.
func (s *Store) allowedLibraryIDs(requested []string) (ids []int, denyAll bool) {
	s.publicMu.RLock()
	allow := s.publicLibraryIDs
	s.publicMu.RUnlock()

	if len(allow) == 0 {
		return nil, true
	}
	req := cleanIDs(requested)
	if len(req) == 0 {
		out := make([]int, len(allow))
		copy(out, allow)
		return out, false
	}
	allowSet := make(map[int]bool, len(allow))
	for _, id := range allow {
		allowSet[id] = true
	}
	out := make([]int, 0, len(req))
	for _, id := range req {
		if allowSet[id] {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, false
}

func cleanIDs(in []string) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
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
