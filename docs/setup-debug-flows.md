# Public Catalog: Setup, Debugging, And Flows

Plugin ID: `silo.public-catalog`. The capability and config table live
in the [README](../README.md) — this doc is the entry point to the operator
runbook and the developer's tour of how requests, tokens, and stats move
through the plugin.

## Where to look

| Doc | Audience | Covers |
| --- | --- | --- |
| [operations.md](operations.md) | operator | Postgres schema/role/grants, first install, listener modes, restarts, secret rotation, backups. |
| [request-flows.md](request-flows.md) | developer + operator | Per-route walkthroughs: landing, `?page=`, catalog gate, item detail, admin API, federated sources. |
| [tokens-and-auth.md](tokens-and-auth.md) | both | HMAC token shape, cookie vs `?token=`, scope intersection, password hashing, what rotation invalidates. |
| [architecture.md](architecture.md) | developer | Package layout, local-store fast path vs SDK fallback, caches, image-URL resolution, federated source plugin. |
| [debugging.md](debugging.md) | operator | Specific symptoms (stats not refreshing, bypass link 401, broken posters), what to check, what log lines to grep. |
| [public-vs-admin.md](public-vs-admin.md) | both | Quick split of which surfaces are anonymous-visitor-facing vs operator-only. |

## Two-minute orientation

The plugin serves three audiences from one Go binary:

1. **Anonymous visitors** see the landing page (`/`) and — once they enter
   the catalog password or follow a signed bypass link — a catalog
   browser (`/catalog`, `/item/{id}`). Their entire experience runs on
   the embedded SPA in `internal/server/public/dist`.
2. **Operators** configure the plugin through the Silo admin UI
   (`/admin` mounted via `http_routes.v1` with `access: admin`) and the
   admin JSON API at `/api/admin/*`. The Silo host strips inbound
   `X-Silo-*` identity headers from public requests, so the role
   check in `requireAdmin` (`internal/server/middleware.go`) is
   trustworthy.
3. **Other plugins** (companion ebook/audiobook installations) are
   called back via `host.CallPluginJSON` to federate stats and catalog
   listings; see `internal/server/sources.go::RuntimeHostSource`.

The plugin owns one Postgres schema (default `public_catalog`) with three
tables — `app_config` (singleton JSONB), `saved_catalog_links`,
`provider_image_cache` — and reads several `public.*` tables in the host
schema for catalog browsing. That cross-schema reach is the single
biggest source of first-install pain; see
[operations.md](operations.md#postgres-role-and-grants).

## Quick verification after a config change

1. `PATCH /api/admin/config` from the admin UI succeeded.
2. `GET /api/admin/config` shows the new values (secrets redacted —
   `TokenSecret` and `CatalogPassword` are stripped by
   `handlers_admin_api.go::redactConfig`).
3. `GET /api/public/stats` returns non-empty counts when the host
   library has matched items.
4. `GET /catalog` with a known-good `?token=` (or after entering the
   password) returns HTTP 200 and a non-empty `initialItems` in the
   bootstrap.
5. Tail the plugin process log; on listener changes you'll see either
   `standalone http listener bound` (clean) or
   `standalone_http_listen change ... requires a plugin restart` (which
   means your change didn't take effect — restart the plugin).
