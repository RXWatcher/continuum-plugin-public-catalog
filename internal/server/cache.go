package server

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimehost"
)

// statsCache memoises GetCatalogStats responses per library-id-set for
// a short TTL. The catalog stats query is the hottest path on the
// landing page and changes slowly relative to the cache TTL.
type statsCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]statsCacheEntry
}

type statsCacheEntry struct {
	expires time.Time
	stats   *runtimehost.CatalogStats
}

// catalogCache memoises rendered catalog media + filters pages for a
// short TTL. Cache misses fall through to the store; hits are signalled
// to clients via the X-Public-Catalog-Cache: HIT header.
type catalogCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	media   map[string]catalogMediaCacheEntry
	filters map[string]catalogFiltersCacheEntry
}

type catalogMediaCacheEntry struct {
	expires time.Time
	resp    *store.CatalogMediaResponse
}

type catalogFiltersCacheEntry struct {
	expires time.Time
	resp    *store.CatalogFilters
}

func newCatalogCache(ttl time.Duration) *catalogCache {
	return &catalogCache{
		ttl:     ttl,
		media:   map[string]catalogMediaCacheEntry{},
		filters: map[string]catalogFiltersCacheEntry{},
	}
}

func (c *catalogCache) getMedia(key string) (*store.CatalogMediaResponse, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.media[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(c.media, key)
		}
		return nil, false
	}
	return entry.resp, true
}

func (c *catalogCache) setMedia(key string, resp *store.CatalogMediaResponse) {
	if c == nil || key == "" || resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.media[key] = catalogMediaCacheEntry{expires: time.Now().Add(c.ttl), resp: resp}
	if len(c.media) > 256 {
		c.pruneLocked()
	}
}

func (c *catalogCache) getFilters(key string) (*store.CatalogFilters, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.filters[key]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(c.filters, key)
		}
		return nil, false
	}
	return entry.resp, true
}

func (c *catalogCache) setFilters(key string, resp *store.CatalogFilters) {
	if c == nil || key == "" || resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.filters[key] = catalogFiltersCacheEntry{expires: time.Now().Add(c.ttl), resp: resp}
	if len(c.filters) > 128 {
		c.pruneLocked()
	}
}

func (c *catalogCache) pruneLocked() {
	now := time.Now()
	for key, entry := range c.media {
		if now.After(entry.expires) {
			delete(c.media, key)
		}
	}
	for key, entry := range c.filters {
		if now.After(entry.expires) {
			delete(c.filters, key)
		}
	}
}

func catalogMediaCacheKey(libraryIDs, mediaTypes []string, query, genre string, yearMin, yearMax int, sort string, descending bool, pageSize int, pageToken string) string {
	return strings.Join([]string{
		"media",
		cacheListKey(libraryIDs),
		cacheListKey(mediaTypes),
		strings.TrimSpace(query),
		strings.TrimSpace(genre),
		strconv.Itoa(yearMin),
		strconv.Itoa(yearMax),
		sort,
		strconv.FormatBool(descending),
		strconv.Itoa(pageSize),
		pageToken,
	}, "\x00")
}

func catalogFiltersCacheKey(libraryIDs, mediaTypes []string) string {
	return strings.Join([]string{"filters", cacheListKey(libraryIDs), cacheListKey(mediaTypes)}, "\x00")
}

func cacheListKey(in []string) string {
	out := cleanList(in)
	sort.Strings(out)
	return strings.Join(out, ",")
}

func statsCacheKey(libraryIDs []string) string {
	ids := cleanList(libraryIDs)
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
}
