package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

// currentHost resolves the live host SDK client (or nil if the plugin
// host hasn't connected yet). Held as a function on Deps so tests can
// supply a stub and so the live SDK can be lazily-bound after Configure.
func currentHost(d Deps) Host {
	if d.Host == nil {
		return nil
	}
	return d.Host()
}

// safeGetCatalogStats wraps host.GetCatalogStats with a panic
// recovery. Some host versions surface a panic when the catalog hasn't
// been initialised yet, and we don't want the stats endpoint to crash
// the whole request handler when that happens.
func safeGetCatalogStats(ctx context.Context, host Host, libraryIDs []string) (stats *runtimehost.CatalogStats, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stats = nil
			err = fmt.Errorf("get catalog stats panicked: %v", recovered)
		}
	}()
	return host.GetCatalogStats(ctx, libraryIDs)
}

func (c *statsCache) get(ctx context.Context, hostFn func() Host, libraryIDs []string, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	key := statsCacheKey(libraryIDs)
	now := time.Now()
	c.mu.Lock()
	if c.entries != nil {
		if ent, ok := c.entries[key]; ok && now.Before(ent.expires) {
			stats := ent.stats
			c.mu.Unlock()
			return stats, nil
		}
	}
	c.mu.Unlock()

	host := currentHost(Deps{Host: hostFn})
	if host == nil {
		return nil, http.ErrServerClosed
	}
	stats, err := safeGetCatalogStats(ctx, host, libraryIDs)
	if err != nil {
		return nil, err
	}
	stats, err = withSourceStats(ctx, host, stats, sources)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]statsCacheEntry{}
	}
	c.entries[key] = statsCacheEntry{expires: now.Add(c.ttl), stats: stats}
	c.mu.Unlock()
	return stats, nil
}

// withSourceStats walks the configured CatalogSources and adds their
// counts to the base catalog stats. Source failures are silently
// dropped: a federated source being down should not break the host's
// stats response.
func withSourceStats(ctx context.Context, host Host, base *runtimehost.CatalogStats, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	if base == nil {
		base = &runtimehost.CatalogStats{}
	}
	for _, src := range sources {
		if src == nil {
			continue
		}
		stats, err := src.Stats(ctx, host)
		if err != nil {
			continue
		}
		base.TotalItems += stats.TotalItems
		base.MediaTypeCounts = append(base.MediaTypeCounts, stats.MediaTypeCounts...)
		base.LibraryCounts = append(base.LibraryCounts, stats.LibraryCounts...)
	}
	return base, nil
}

func appendSourceCatalogs(ctx context.Context, host Host, base *runtimehost.ListLibraryMediaResponse, sources []CatalogSource, req runtimehost.ListLibraryMediaRequest) *runtimehost.ListLibraryMediaResponse {
	if base == nil {
		base = &runtimehost.ListLibraryMediaResponse{}
	}
	for _, src := range sources {
		if src == nil || !mediaTypeRequested(req.MediaTypes, src.MediaType()) {
			continue
		}
		sourceReq := req
		sourceReq.MediaTypes = []string{src.MediaType()}
		if sourceReq.PageSize <= 0 || sourceReq.PageSize > 12 {
			sourceReq.PageSize = 12
		}
		resp, err := src.List(ctx, host, sourceReq)
		if err != nil || resp == nil {
			continue
		}
		base.Items = append(base.Items, resp.Items...)
		base.TotalCount += resp.TotalCount
	}
	return base
}

func mediaTypeRequested(requested []string, mediaType string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, v := range requested {
		if v == mediaType {
			return true
		}
	}
	return false
}
