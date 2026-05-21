# Debugging Runbook

Symptom-first guide for the issues operators actually hit. Each section
explains what to check, in what order, and which file/handler is doing
the work.

## Stats not refreshing on the landing page

**Where the data comes from**: `GET /api/public/stats` →
`publicCatalogStats` → either `ConfigStore.CatalogStats` (DB fast path)
or `host.GetCatalogStats` (SDK fallback) → optional federated sources.
Results go through a 30-second in-memory cache keyed by sorted library
IDs.

Checks in order:

1. **Cache TTL**: stats are cached for 30s (`StatsCacheTTL`). If you
   imported items 10 seconds ago and the page is still showing the old
   count, wait it out or restart the plugin to bust the cache.
2. **Local-DB fast path returning zero**: open psql with the plugin
   role and run a quick count:
   ```sql
   SELECT COUNT(DISTINCT content_id)
     FROM public.media_items
    WHERE status = 'matched';
   ```
   If this is zero from inside the plugin role but non-zero from the
   host's role, you're missing the `GRANT SELECT ON public.media_items`
   — see [operations.md](operations.md#postgres-role-and-grants).
   Symptom-wise, the plugin then falls through to the SDK fallback,
   which may return a different (possibly smaller) total because the
   SDK does not expose every match status.
3. **Federated source down**: `host.CallPluginJSON` failures from a
   companion ebook/audiobook plugin are silently dropped
   (`host.go::withSourceStats`). If your "Total items" looks low,
   inspect the companion plugin's logs for `/api/v1/public-catalog/stats`
   errors.
4. **Scope filter on the landing page**: a `?page=<saved-link>` URL
   inherits the saved link's claim, which can cut media types out via
   `scopeCatalogStats`. View the same page without `?page=` to confirm.
5. **Library ID mismatch**: stats are keyed by media_folders.id (an
   int as text). If the host renames or replaces a library, old library
   IDs stop returning counts.

## Bypass link returns 401 unexpectedly

**Where verification happens**: `catalogClaimsFromRequest` →
`verifyToken` (`token.go`). The HMAC must verify, scope must equal
`"catalog"`, and `exp` (when non-zero) must be in the future.

Checks in order:

1. **Did the `token_secret` rotate?** That's the #1 cause. Any change
   to `Config.TokenSecret` invalidates every existing token. Look at
   the plugin log around the time the link started failing for a
   Configure RPC. There is no "soft" rotation; if you accidentally
   regenerated the secret, you have to re-mint saved links.
2. **Bad copy/paste in the URL**: the token contains a `.` separator;
   if a mail client URL-decoded the `?token=` value, the trailing
   base64 padding might be stripped. Try the saved-link record from
   `GET /api/admin/catalog-links` and re-share the canonical `url`.
3. **Token was minted by a different installation**: each plugin
   installation has its own `token_secret`. A token from staging won't
   verify in production.
4. **Expired**: bypass tokens currently have no `exp` claim
   (`hCreateToken` doesn't set one), so this should be impossible —
   but session cookies inherit `exp=0` too, so neither should expire
   on time. If you're seeing `invalid_token: expired` in logs,
   something's writing the `exp` claim manually.

## Login fails with the password the operator just set

Checks in order:

1. **`CatalogPasswordRequired` is false**: `hCatalogLogin` issues a
   session anyway in this case, so a successful response isn't proof
   the password was accepted. To verify the password path, set
   `CatalogPasswordRequired=true` (admin UI) and try again.
2. **Hash didn't write**: `PATCH /api/admin/config` with an empty
   `catalog_password` is interpreted as "no change" so the form must
   actually contain the new plaintext. The hash is in
   `app_config.data->>'catalog_password_hash'`; confirm it changed.
3. **Cookie `Secure` flag dropped by a proxy**: behind a TLS proxy that
   doesn't forward `X-Forwarded-Proto: https`, the plugin sees
   `r.TLS == nil` and DOES NOT set `Secure`. Browsers may still send
   it on the same-origin subsequent request, but if your proxy is
   rewriting `Set-Cookie` it can strip the cookie entirely. Inspect a
   successful login response in DevTools to confirm `Set-Cookie:
   public_catalog_auth=...` is present and the browser accepted it.
4. **Legacy plaintext lingering**: `VerifyCatalogPassword` matches a
   stored plaintext `catalog_password` field too. If both that and a
   hash are set, the plaintext can shadow the new hash for one login —
   then it's rehashed in the background. Inspect
   `app_config.data->>'catalog_password'` directly; if non-empty,
   manually clear it.

## Posters/backdrops broken in the catalog

**Where URLs come from**: the local store returns TMDB/TVDB paths;
`overlayCatalogMediaImages` batches them through
`host.ResolveCatalogImageURLs("card")`. For item-detail pages, the
slower fallback `enrichCatalogItemImages` calls
`host.ListLibraryMedia` and matches by ID.

Checks in order:

1. **Host runtime unreachable**: `currentHost(d)` returns nil if the
   host SDK hasn't connected yet (e.g. during a host upgrade). The
   overlay silently no-ops, leaving the raw TMDB/TVDB paths which the
   SPA can't render. Look for `failed to ... continuum host is not
   connected` lines in the log.
2. **Missing GRANT on `public.media_items.poster_path`**:
   `CatalogMediaImagePaths` reads `mi.poster_path` and friends. Without
   `GRANT SELECT`, the query errors and the overlay short-circuits.
3. **Image cache TTL expired but host can't refresh**: the
   `provider_image_cache` row has `expires_at`. After expiry, fresh
   requests go through `ResolveCatalogImageURLs`. If the host's
   provider client is rate-limited or down, posters can blink out for
   the cache lifetime.
4. **`?token` scope hides the matching item**: `enrichCatalogItemImages`
   passes `claims.LibraryIDs` to `host.ListLibraryMedia`. If the token
   scopes out the library that holds the canonical series record, the
   detail-page enrichment finds zero matches and leaves
   `PosterURL`/`BackdropURL` blank.

## Listener won't bind

`net.Listen` is called synchronously in the Configure RPC. Failures
surface as a config-error in the admin UI and a line in the plugin log.

| Error | Meaning | Fix |
| --- | --- | --- |
| `bind standalone listener "...": ... address already in use` | Port collides with another process. | Pick a different `public_port`, or stop the colliding process. |
| `bind standalone listener "...": ... permission denied` | Port < 1024 without CAP_NET_BIND_SERVICE. | Use `>= 1024`, or grant the capability to the plugin binary. |
| `standalone_http_listen "x" is invalid: must include host:port` | Bad address shape. | Use `:8090`, `127.0.0.1:8090`, or `[::1]:8090`. |
| `standalone_http_listen change from "x" to "y" requires a plugin restart` | The listener was bound earlier and the address changed. | Restart the plugin (the SDK doesn't expose a way to rebind in-place). |

The wildcard-bind warning (`standalone listener bound to ALL interfaces`)
is informational — it's not an error, but it usually means the operator
forgot to set the loopback host.

## Admin UI shows "config storage is not configured"

The handler check is `if d.ConfigStore == nil`. That fires when
`applyConfig` errored before setting up the chi handler — usually
because the migration step failed or the pool couldn't open.

Look for:

```
migrate: ...
parse database_url: ...
connect database: ...
bootstrap config: ...
```

in the plugin log. The Configure RPC returns the same error to the
host so the admin UI shows it too — the "not configured" message is the
fallback when an older config (without a working DB) is still in
memory.

## Saved link's HTML doesn't appear on the landing page

`?page=<name>` looks up `saved_catalog_links` by exact name match
(`GetCatalogLinkByName`). Checks:

1. The query param value is the link's `name`, not its `token` or `id`.
2. The link exists: `GET /api/admin/catalog-links` should show it.
3. The link's `html` field is non-empty; with an empty `html` the
   landing page falls back to the operator-published HTML.
4. The link's `url` is what populates `catalogHref` on the landing
   page; an empty URL falls back to plain `catalog`.

## SPA shows old assets after deploy

The SPA is served from the embedded `internal/server/public/dist`
filesystem. There is no client-side cache-busting beyond what Vite
emits — the `index.html` references hashed asset names. A stale-looking
page after a release usually means:

1. The plugin binary on disk wasn't actually replaced — `ls -la` the
   binary path the host launches.
2. Behind a reverse proxy that caches `/assets/*`, the new hashed
   filenames are 404ing from the proxy cache. The plugin sets
   no-store on session-gated paths but NOT on `/assets/*`. Clear the
   proxy cache or set a `Cache-Control` on the proxy for `*.html`.

## Cross-schema reads return zero rows

If `CatalogStats` returns `TotalItems=0` but you know the library has
items:

1. Confirm `media_items.status = 'matched'` for some rows — unmatched
   items are excluded by every query.
2. Confirm `media_folders.enabled` is true for the relevant folders
   (`COALESCE(mf.enabled, true)`).
3. Confirm the `library_ids` filter passed in. The query expects
   integer IDs; `cleanIDs` (`store/store.go`) silently drops anything
   that doesn't parse, so a stringified UUID will leave the filter
   empty (matching everything) which is usually NOT what the operator
   intended.

## Operator log greps

| Pattern | Meaning |
| --- | --- |
| `configured public-catalog plugin` | Configure RPC succeeded. |
| `standalone http listener bound` | The plugin owns its own port. |
| `standalone listener bound to ALL interfaces` | Wildcard bind warning. |
| `draining standalone http listener` | Graceful shutdown started (SIGINT/SIGTERM). |
| `password rehash failed` | Legacy plaintext upgrade attempt failed. The user's session was still issued. |
| `migrate: ...` | Migration error during Configure. The plugin won't serve traffic until fixed. |
