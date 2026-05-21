# Request Flows

Per-route walkthroughs for every surface the plugin exposes. File and
function references point into `internal/server/`.

## SPA shell

Every page route (`/`, `/catalog`, `/item/{id}`, `/admin`) goes through
`spa.go::writePublicApp`. The handler reads the embedded
`public/dist/index.html`, marshals a `publicBootstrap` JSON struct,
splices it into `%PUBLIC_CATALOG_BOOTSTRAP%`, sets `data-theme=...` on
`<html>`, and rewrites `./assets/` to `../assets/` for `/item/*` routes
so relative asset paths still resolve. The SPA reads
`window.__PUBLIC_CATALOG_BOOTSTRAP` and routes accordingly.

`adminTheme` (same file) picks the theme from the `?theme=` query, the
`X-Continuum-Theme` request header (set by the host's proxy), or the
`X-Continuum-User-Theme` header, falling back to
`midnight-cinema` when no theme is supplied. Themes are HTML-escaped
before being inserted as an attribute.

## Landing page

`GET /` → `handlers_landing.go::hLanding`.

1. If `?page=<name>` is present, look up the named saved link via
   `ConfigStore.GetCatalogLinkByName`. When found, the link's `html`
   replaces the default `published_html`, and the link's `url`
   overrides the catalog href the SPA renders. This is how one install
   can publish multiple framed landing pages (one per saved bypass
   link).
2. Extract claims from the request (cookie or `?token=`) so library/
   media-type scope is applied to stats.
3. Aggregate stats via `publicCatalogStats` (see [architecture.md](
   architecture.md#stats-pipeline)).
4. Apply `scopeCatalogStats` to filter `MediaTypeCounts` /
   `LibraryCounts` down to whatever the active claim allows.
5. Pick the operator HTML:
   `defaultString(customHTML, publishedHTML(...))` — saved-link HTML
   wins, falling back to the operator-published HTML, falling back to
   the manifest seed in `Config.AdHTML`. `adHTML` wraps an empty
   string in a static fallback paragraph so the page never renders an
   empty card.
6. Write the SPA shell with `Mode: "landing"`.

The SPA sanitises the embedded HTML with `isomorphic-dompurify` before
rendering — never trust round-tripped operator HTML to be safe just
because the operator wrote it.

## Catalog page

`GET /catalog` → `hCatalogPage`.

1. Resolve claims via `catalogClaimsFromRequest`:
   - `?token=...` → verify HMAC and decode tokenClaims.
   - else `public_catalog_auth` cookie → verify HMAC.
   - else if `CatalogPasswordRequired=false` → permissive
     `Scope: "catalog"` claim so the page renders without a gate.
   - else → no claim, page is rendered with `AuthRequired=true` and
     HTTP 401.
2. Always render the SPA shell — even on 401 — so the React app can
   show the login prompt. The 401 status lets a non-browser client
   (curl, link-checkers) see the gate.
3. When a claim resolves AND a library is chosen by
   `initialCatalogLibraryID` (first allowed library with items, or the
   `?libraryId` query param if allowed by scope), pre-fetch the first
   page of items and the filters payload so the SPA paints without an
   extra round-trip:
   - Look up the media-cache key for the first 60 items.
   - Cache hit → use it.
   - Cache miss → call `ConfigStore.CatalogMedia`, run
     `overlayCatalogMediaImages` to fill missing poster/backdrop URLs,
     cache the result, attach to bootstrap.
4. Same dance for filters (`ConfigStore.CatalogFilters` + filters
   cache).

The `Token` bootstrap field is the raw `?token=` (or empty); the SPA
forwards it on every `/api/catalog/*` call so a bypass link doesn't
need a cookie.

## Item detail

`GET /item/{id}` → `hItemPage` renders the SPA with `Mode: "detail"`.
The actual item lookup is a JSON call from the SPA, not server-rendered.

The JSON call is `GET /api/catalog/items/{id}` →
`hCatalogItemDetail`:

1. Resolve claims (must be present, else 401).
2. `ConfigStore.CatalogItemDetail` reads from `public.media_items` and
   joins seasons/episodes when the type is `series`/`season`/`episode`.
3. `enrichCatalogItemImages` calls `host.ListLibraryMedia` with the
   item's title (or its series title) and walks the response until
   `candidate.MediaID == matchID`, copying poster/backdrop URLs over.
   This is the slow fallback path; the fast path (`overlayCatalogMediaImages`)
   batches `host.ResolveCatalogImageURLs` calls for catalog lists.

## Catalog data API

All `/api/catalog/*` endpoints share the claim-resolution behaviour
above and return 401 with `catalog_auth_required` if there's no valid
session.

| Route | Handler | Purpose |
| --- | --- | --- |
| `GET /api/catalog/media` | `hCatalogMedia` | Paginated library browse. |
| `GET /api/catalog/filters` | `hCatalogFilters` | Genre / year ranges for the current scope. |
| `GET /api/catalog/items/{id}` | `hCatalogItemDetail` | Single item with poster/backdrop enrichment. |
| `GET /api/catalog/items/{id}/seasons` | `hCatalogSeriesSeasons` | Seasons for a series. |
| `GET /api/catalog/series/{id}/seasons/{season}/episodes` | `hCatalogSeasonEpisodes` | Episodes for a season. |

### Source preference in `hCatalogMedia`

`hCatalogMedia` runs three branches:

1. **Local store fast path** — when every requested media type is
   `movie`, `tv`, `series`, or `episode` (`directCatalogMediaTypes`) and
   the store is configured, query the DB directly. Cache key includes
   library IDs, media types, query, genre, year range, sort, descending
   flag, page size and page token. On hit, set the
   `X-Public-Catalog-Cache: HIT` response header.
2. **Single-type federated source** — for ebook/audiobook requests with
   exactly one media type, call the matching `CatalogSource` (see
   `sources.go::RuntimeHostSource`).
3. **General SDK fallback** — `host.ListLibraryMedia`. For the very
   first page of a request, mix in source results via
   `appendSourceCatalogs` so the SPA gets a few rows from the federated
   plugins alongside the SDK rows.

Scope intersection happens up front: `mergeMediaTypes` and
`mergeLibraryIDs` intersect the token's allowed scope with the
request's filters. A request that asks for something the token doesn't
allow returns 400 (`invalid_media_type` / `invalid_library`) instead of
silently dropping.

## Catalog login

`POST /api/public/catalog-login` → `hCatalogLogin`.

1. Decode `{password: "..."}`.
2. If `CatalogPasswordRequired=false`, issue a session cookie anyway
   (so the SPA always gets a consistent ok response).
3. If no password and no hash are configured, return 401
   `invalid_password` — the operator should toggle
   `CatalogPasswordRequired=false` instead of "no password = open".
4. `VerifyCatalogPassword` bcrypt-compares the supplied password
   against the stored hash. On legacy-plaintext match, kick off a
   bcrypt rehash in a background goroutine and issue the session.
5. Success → sign a `tokenClaims{Scope: "catalog"}` token and set it as
   the `public_catalog_auth` cookie (`HttpOnly`, `SameSite=Lax`,
   `Secure` when TLS is present).

## Admin API

All routes under `/api/admin/*` and `/admin` go through `requireAdmin`
(`middleware.go`). The check trusts `X-Continuum-User-Id` and
`X-Continuum-User-Role` because the host strips those headers from
public requests; if they reach the plugin they came from the host's
authenticated routing.

| Route | Notes |
| --- | --- |
| `GET /api/admin/config` | Returns the config with `TokenSecret` and `CatalogPassword` stripped. The hash stays so the UI can show "password is set". |
| `PATCH /api/admin/config` | Validates through `runtime.NormalizeAppConfig`. Live listener changes are rejected with a clear error. Empty `CatalogPassword` is interpreted as "no change". |
| `DELETE /api/admin/catalog-password` | Clears hash + force `CatalogPasswordRequired=false`. |
| `GET /api/admin/html-section` | Returns the current published HTML. |
| `PUT /api/admin/html-section` | Writes `published_html`. 200 KB hard cap; bigger pastes return 400 `html_too_large`. |
| `POST /api/admin/catalog-token` | Mints a bypass token. Body fields: `hours`, `libraryIds`, `mediaTypes`, `saveName`, `html`. When `saveName` is set, also writes a `saved_catalog_links` row. Returns `{token, url, savedLink?}`. The `url` is absolute when `public_base_url` (or a non-wildcard standalone listener) resolves; otherwise relative. |
| `GET /api/admin/catalog-links` | Lists saved links. |
| `DELETE /api/admin/catalog-links/{id}` | Deletes a saved link. 404 when the row is gone. |

## Public stats

`GET /api/public/stats` → `hStats`. Anonymous, used by the landing page
and the catalog header. Failures are swallowed: an empty
`store.CatalogStats{}` JSON body is returned instead of a 5xx so the
landing page doesn't bork.

## Federated plugin call

For ebook/audiobook stats and listings, the plugin issues a JSON-typed
call into a companion plugin's public-catalog endpoint:

```
GET /api/v1/public-catalog/stats
GET /api/v1/public-catalog/media?...
```

via `host.CallPluginJSON` with the configured installation ID. The
companion plugin must expose those routes and return
`runtimehost.CatalogStats` / `runtimehost.ListLibraryMediaResponse`
shapes. Failures from federated sources are silently dropped so a
down companion never breaks the main response (`host.go::withSourceStats`).
