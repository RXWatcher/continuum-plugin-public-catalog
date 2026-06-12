# Public Catalog for Silo

`silo.public-catalog` is a public-facing advertising and catalog plugin for a Silo deployment. It serves an anonymous landing page with library stats and operator-edited HTML, a searchable catalog gated by a shared password (or a per-recipient bypass token), and an admin console for operators to manage links, content, and settings.

## Category

Lives under **Sharing**, alongside [`silo.guest-pass`](https://github.com/RXWatcher/silo-plugin-guest-pass) (per-item tightly scoped shares). Where guest-pass exposes a single item to a single recipient, public-catalog publishes the whole library to anonymous visitors and uses signed catalog tokens to scope what each shared link can browse.

## Capabilities

| Type | ID | Purpose |
| --- | --- | --- |
| `http_routes.v1` | `public-catalog` | Public landing page, signed-token catalog browser, public stats API, and operator admin console. |

The plugin registers the following routes:

| Path | Method | Access | Notes |
| --- | --- | --- | --- |
| `/` | `GET` | public | Landing page (stats + published HTML). |
| `/catalog` | `GET` | public | Catalog browser SPA (password or `?token=`). |
| `/item/*` | `GET` | public | Item detail SPA route. |
| `/api/public/*` | `*` | public | Anonymous stats, catalog login. |
| `/api/catalog/*` | `*` | public | Gated catalog JSON API (cookie or token claim). |
| `/api/admin/*` | `*` | admin | Admin JSON API (config, links, HTML, tokens). |
| `/admin` | `GET` | admin | Admin console SPA, navigable from Silo's admin nav as "Public Catalog". |

## Dependencies

- Reads library data from the Silo host via the SDK's `ListLibraryMedia`, `GetCatalogStats`, and the newer `ResolveCatalogImageURLs` method — used in `internal/server/handlers_catalog.go::overlayCatalogMediaImages` to resolve poster/backdrop paths returned by the local store fast path into signed, runtime-host-served image URLs.
- Falls back to the host SDK for catalog browsing when the local store fast path does not apply.
- Reads `public.media_items`, `public.media_files`, `public.episodes`, `public.seasons`, `public.media_folders`, `public.media_item_libraries`, and `public.episode_libraries` directly from the host's Postgres schema for episode-level browsing, series/season expansion, and quality-bucket aggregation. These require explicit `GRANT SELECT` to the plugin role.
- Federates stats and single-type browsing for ebooks and audiobooks from companion plugin installations via `host.CallPluginJSON`.

Host: [`Silo-Server/silo-server`](https://github.com/Silo-Server/silo-server). SDK: [`Silo-Server/silo-plugin-sdk`](https://github.com/Silo-Server/silo-plugin-sdk).

## External services

- **Postgres** (required). The plugin owns a dedicated schema (default `public_catalog`) with three tables managed by `golang-migrate`:
  - `app_config` — singleton JSONB row holding every plugin setting, the auto-generated HMAC token secret, the bcrypt-hashed catalog password, and the operator-published landing HTML.
  - `saved_catalog_links` — named bypass links the admin creates.
  - `provider_image_cache` — short-lived cache of resolved poster / backdrop URLs.

Migrations run automatically on plugin start; the operator only needs to create the schema, role, and cross-schema grants on `public.*` listed above.

## Public surfaces

- **Landing page** (`/`) — server-rendered SPA shell with a bootstrap JSON payload. Shows library stats and the operator-published HTML block. A `?page=<name>` query parameter swaps in the HTML and catalog href stored on a named saved link, so one operator can publish multiple framed landing pages from a single install.
- **Operator-controlled HTML** — `/api/admin/html-section` (GET/PUT) edits a free-form HTML block stored in `app_config.published_html`, with a 200 KB cap. The SPA sanitises operator HTML with `isomorphic-dompurify` before rendering. A manifest-supplied `ad_html` seed is used until the operator publishes their own.
- **Catalog browser** (`/catalog`) — gated by either:
  - a session cookie (`public_catalog_auth`) issued by `/api/public/catalog-login` after a bcrypt password check, or
  - a signed bypass token (`?token=...`) created by the admin.
  Both carry the same HMAC-signed claim shape: `scope=catalog`, optional `library_ids` and `media_types` scope, and optional expiry. Stats and listings are scoped down server-side to whatever the active claim allows.
- **Signed bypass links** — `POST /api/admin/catalog-token` mints a catalog token, optionally saves it as a named link with its own embedded HTML, and returns the absolute URL to share.
- **Federated stats** — totals can include ebook and audiobook counts pulled from companion plugin installations identified by `ebook_installation_id` / `audiobook_installation_id`.

## Configuration

| Key | Required | Description |
| --- | --- | --- |
| `database_url` | yes | Postgres DSN for the plugin schema. |
| `token_secret` | no | HMAC secret for catalog tokens. Auto-generated into `app_config` on first run if missing; manifest-supplied values must be at least 32 chars. Rotating it invalidates every outstanding bypass link and login cookie. |
| `public_port` | no | Standalone public listener port (default `8090`). Synthesises `:8090` if `standalone_http_listen` is unset. |
| `standalone_http_listen` | no | Advanced listener address (e.g. `127.0.0.1:8090`). Wildcard binds log a warning — prefer loopback behind a reverse proxy. Changing this at runtime requires a plugin restart. |
| `public_base_url` | no | Absolute URL embedded in generated bypass links and emitted by the admin API. |
| `catalog_password` | no | Plaintext password input. The plugin hashes with bcrypt on the first write and never persists plaintext. A legacy plaintext value left in the DB is rehashed transparently on a successful login. |
| `ad_html` | no | First-run seed for the landing HTML. After the operator edits via `/admin`, the manifest value is ignored. |
| `token_ttl_hours` | no | Default expiry for admin-minted bypass tokens (default `168`, one week). |
| `ebook_installation_id` | no | Installation ID of a companion ebooks plugin used for federated stats and single-type browsing. |
| `audiobook_installation_id` | no | Installation ID of a companion audiobooks plugin used for federated stats and single-type browsing. |

Configuration shape lives in `internal/runtime/runtime.go::Config` and round-trips as the singleton `app_config.data` JSONB row, so adding a field there extends the persisted shape automatically. Admin edits flow through `PATCH /api/admin/config`; password management has dedicated endpoints (`DELETE /api/admin/catalog-password` clears the stored hash and turns the gate off).

## Detailed docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Build and release

```bash
make build   # web-build (Vite) then go build of cmd/silo-plugin-public-catalog
make test    # go test ./... + pnpm run test in web/
```

The frontend lives in `web/` (React 19, TypeScript, Vite, Tailwind v4, Radix, isomorphic-dompurify, pnpm) and emits into `internal/server/public/dist`, which is `//go:embed`-ed into the binary.

CI builds linux-amd64 binaries on push to main via the reusable workflow in [RXWatcher/silo-plugin-repository](https://github.com/RXWatcher/silo-plugin-repository) and publishes them to the catalog at [`./binaries/`](https://github.com/RXWatcher/silo-plugin-repository/tree/main/binaries).
