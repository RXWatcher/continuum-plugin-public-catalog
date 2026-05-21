# Architecture

A developer's tour of the plugin's internal packages and the
non-obvious paths data takes between them.

## Process layout

```
cmd/continuum-plugin-public-catalog/main.go   <- entrypoint, listener bind, lifecycle
internal/runtime                              <- Configure RPC + Config struct
internal/migrate                              <- embedded SQL migrations
internal/store                                <- Postgres access (own schema + public.*)
internal/server                               <- chi router, handlers, SPA shell
internal/httproutes                           <- adapter exposing chi router to the host
```

`main.go` constructs the runtime server (`pluginrt.New`) with an
`applyConfig` callback. The host calls `Configure`, which lands in
`runtime.Server.Configure`; that normalises the manifest bag into a
`Config`, hands it to `applyConfig`, and stores it. Inside
`applyConfig`:

1. Run embedded migrations (`migrate.Run`).
2. Open a pgxpool against the DSN.
3. Run `store.Bootstrap` to materialise defaults, generate the token
   secret, and upgrade any legacy plaintext password.
4. Build the chi handler with `server.New(Deps{...})` and swap it into
   the `httproutes.Server` adapter atomically (so a Configure during
   live traffic doesn't tear down handlers in flight).
5. Bind the standalone listener exactly once (`standaloneOnce`).
6. Close the previous pool, if any (`poolPtr.Swap(pool).Close()`).

The host SDK is reached via `sdkruntime.Host()` lazily — `Deps.Host`
is a function so tests can stub it and so the SDK can be bound after
Configure.

## Package map

### `internal/server` files

The `server.go` doc comment gives the canonical map. Highlights:

- `server.go` — the chi router, `Deps`, `Host` and `ConfigStore`
  interfaces. The interfaces let tests stub Postgres and the SDK.
- `handlers_landing.go` — `/`, `/catalog`, `/item/{id}`, `/admin` SPA
  entry points. Owns the `?page=` lookup, claim-aware stats scoping,
  and the bootstrap pre-fetch for the catalog page.
- `handlers_catalog.go` — gated catalog JSON API. Owns the local-store
  fast path vs SDK fallback decision and the image-overlay step.
- `handlers_auth.go` — `/api/public/catalog-login` and
  `catalogClaimsFromRequest`.
- `handlers_admin_api.go` — admin JSON API. `redactConfig` drops
  secrets before any GET.
- `middleware.go` — `securityHeaders` (CSP, frame-ancestors, no-store
  on session-gated paths) and `requireAdmin` (trusts host-stamped
  identity headers).
- `spa.go` — embedded SPA + `publicBootstrap` shape + theme injection.
- `stats.go` / `host.go` — stats aggregation, fast path,
  source federation, panic-safe host calls.
- `sources.go` — federated source interface and the runtime-host
  implementation used for ebooks/audiobooks.
- `cache.go` — TTL-bounded in-memory caches for stats, catalog media
  pages, and catalog filters.
- `token.go` — HMAC sign/verify (the only crypto in the server
  package).
- `validation.go` — query-param normalisation, scope intersection,
  media-type vocabulary.

### `internal/store` files

Each file owns one bounded concern so handlers don't see a single
1200-line surface:

- `store.go` — pool wrapper, `ErrNotFound`, helpers.
- `config.go` — `GetConfig`, `UpdateConfig`, `Bootstrap`,
  `ClearCatalogPassword`, `RehashCatalogPassword`.
- `password.go` — bcrypt hashing + legacy plaintext compare.
- `saved_links.go` — `SaveCatalogLink`, `ListCatalogLinks`,
  `GetCatalogLinkByName`, `DeleteCatalogLink`.
- `catalog_stats.go` — totals + library counts + 4K/HDR/DV quality
  buckets. Reaches into `public.media_items`, `public.media_files`,
  `public.media_folders`, `public.media_item_libraries`.
- `catalog_media.go` — paginated browse over `public.media_items`
  joined with episodes/series/files.
- `catalog_filters.go` — genre + year range aggregation.
- `catalog_item.go` — single-item detail with episode/season expansion.
- `catalog_series.go` — series-level navigation (seasons + episodes).
- `catalog_quality.go` — quality bucket math (reused by stats).
- `catalog_image.go` — provider image cache + image-path batch lookup.
- `types.go` — DTOs that round-trip to JSON.

## Local store fast path vs SDK fallback

The "fast path" lets the plugin answer common catalog queries without
crossing the gRPC boundary into the host SDK. It exists for two reasons:

1. **Latency** — running a direct SQL query is ~10x cheaper than a
   gRPC round-trip + the host's own query.
2. **Episode-level browsing and quality buckets** that the host SDK's
   `ListLibraryMedia` and `GetCatalogStats` do not yet expose.

`hCatalogMedia` flips into the fast path when:

- `ConfigStore != nil`, AND
- `directCatalogMediaTypes(req.MediaTypes)` returns true — i.e. every
  requested media type is `movie`, `tv`, `series`, or `episode`.

Ebook/audiobook requests skip the fast path entirely and go through a
federated `CatalogSource`. The general SDK fallback covers
mixed-type browses and any unusual filter that we don't translate to
SQL yet.

## Image URL resolution pipeline

The local store stores TMDB/TVDB raw paths
(`public.media_items.poster_path`, etc.) — not signed URLs. The host's
`ResolveCatalogImageURLs` method converts those paths into URLs the
runtime-host serves with appropriate caching.

For catalog list responses, `overlayCatalogMediaImages`
(`handlers_catalog.go`) batches the resolution:

1. Walk the result items, collect MediaIDs whose `PosterURL` or
   `BackdropURL` is empty.
2. `ConfigStore.CatalogMediaImagePaths(missing)` returns a map of
   `MediaID → {PosterPath, BackdropPath}`.
3. Flatten into a single `paths` slice and call
   `host.ResolveCatalogImageURLs(paths, "card")` — one gRPC call per
   page, not one per item.
4. Walk results again and set `PosterURL`/`BackdropURL` from the
   resolved map.

For single-item detail, `enrichCatalogItemImages` uses the slower
fallback: call `host.ListLibraryMedia` with a title-query and walk the
response until the IDs match. That's because the detail handler may
need to fetch a series' poster for an episode lookup, where the path
isn't in `public.media_items` for the episode row.

## Caches

Two short-TTL in-memory caches sit in `Deps`:

| Cache | TTL | Pruning | Cache key | Set by |
| --- | --- | --- | --- | --- |
| `statsCache` | 30s (`StatsCacheTTL`) | lazy on miss | sorted library IDs | `hStats`, landing pre-fetch |
| `catalogCache.media` | 2 minutes | LRU-ish at 256 entries | full query shape | `hCatalogMedia` (fast path only) |
| `catalogCache.filters` | 2 minutes | LRU-ish at 128 entries | library IDs + media types | `hCatalogFilters` |

Cache hits set `X-Public-Catalog-Cache: HIT` (and misses set `MISS`)
so operators can grep proxy logs for cache effectiveness.

The cache TTL is short on purpose: the underlying public.* tables
change as the host scans/imports, and a stale catalog after a fresh
import is the most common operator complaint. If you need to bust the
cache, restart the plugin.

## Stats pipeline

`publicCatalogStats` (`stats.go`) is the canonical aggregation entry:

1. Try `ConfigStore.CatalogStats` — local-DB fast path that reads
   `public.media_items` / `public.media_files`. If it returns any
   non-zero counts, use it and merge federated source stats from
   `withSourceStats`.
2. Otherwise fall back to `host.GetCatalogStats` (`hostStats` →
   `safeGetCatalogStats`, which has a panic-recover for an older
   host bug where uninitialised catalogs panicked).
3. `scopeCatalogStats` filters the result down to a token's
   `MediaTypes` allowlist for the landing page so a recipient with
   movie-only access doesn't see ebook counts.

The aggregator converts between `store.CatalogStats` and
`runtimehost.CatalogStats` (`runtimeStatsFromStore` /
`storeStatsFromRuntime`) so the federated-source pass can be applied
uniformly.

## Federated source plugin contract

A companion plugin (typically `continuum.ebooks` or
`continuum.audiobooks`) must expose:

```
GET /api/v1/public-catalog/stats
GET /api/v1/public-catalog/media?...
```

returning `runtimehost.CatalogStats` and
`runtimehost.ListLibraryMediaResponse` JSON respectively. The plugin
reaches them via `host.CallPluginJSON` with the configured
installation ID. Failure modes (404, 5xx, timeout, JSON decode error)
are silently dropped — a federated plugin going down should never
break the host's stats or the local-store catalog browse.

## SPA bootstrap

`publicBootstrap` (`spa.go`) is the JSON payload baked into
`index.html`'s `%PUBLIC_CATALOG_BOOTSTRAP%`. Fields:

- `mode` — `"landing"`, `"catalog"`, `"detail"`, or `"admin"`.
- `theme` — string; defaults to `midnight-cinema` for the public app
  and to the host-provided theme for the admin app.
- `catalogHref` — relative path the landing page links to (overridden
  by saved-link URLs).
- `customHTML` — sanitised operator HTML / saved-link HTML / fallback.
- `authRequired`, `token` — drive the catalog page's gate.
- `initialStats`, `initialLibraryId`, `initialItems`,
  `initialNextPageToken`, `initialTotalCount`, `initialFilters` —
  pre-rendered catalog data so the SPA doesn't have to spin a loader on
  first paint.

The bootstrap is splice-replaced, NOT JSON.parse'd by the server, so
the JSON is embedded verbatim in HTML; both fields containing HTML
(`customHTML`) and JSON itself must be careful not to break the
parser. The SPA does the final DOMPurify pass on `customHTML`.
