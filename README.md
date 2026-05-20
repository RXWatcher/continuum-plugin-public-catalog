# Public Catalog for Continuum

`continuum.public-catalog` publishes a small public-facing website for a
Continuum deployment. It shows public stats, renders admin-published HTML, and
optionally protects the searchable catalog behind a shared password (or
permanent bypass link).

## Architecture

Catalog SEARCH metadata comes from the host SDK's `ListLibraryMedia` and
`GetCatalogStats`. The plugin **also** reads the host's `public.media_items`,
`public.media_files`, `public.episodes`, `public.seasons`, `public.media_folders`,
`public.media_item_libraries`, and `public.episode_libraries` tables directly
for the things the SDK does not yet expose: episode-level browsing,
detailed quality buckets (4K / HDR / Dolby Vision), per-season episode
lists, and quality counts per media file. Those reads are concentrated in
`internal/store/catalog_*.go` and require explicit `GRANT SELECT`s on the
plugin role (see Database Setup below).

The plugin owns three private tables in its own schema:

- `app_config` (singleton JSONB) — all plugin settings, the token-secret,
  the published landing HTML, and the bcrypt-hashed catalog password.
- `saved_catalog_links` — named bypass links the admin creates.
- `provider_image_cache` — short-lived cache of host-provided poster /
  backdrop URLs.

## Detailed Operations Docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Features

- Standalone public HTTP listener with simple public-port configuration.
- Public landing page with stats and admin-published trusted HTML.
- Public stats sourced from configured Ebooks and Audiobooks portal
  installations via host plugin federation.
- Password-protected catalog browsing with a toggle to open the catalog
  publicly without losing the stored password.
- bcrypt-hashed catalog password with transparent upgrade of any legacy
  plaintext value left over from older installs.
- Admin endpoint for generating permanent catalog bypass links.
- Admin HTML section editor for the public landing page (now persisted
  in the database instead of a file next to the binary).
- Inbound public requests strip Continuum identity headers before routing.

## Configuration

| Key | Required | Description |
|---|---|---|
| `database_url` | yes | Postgres DSN for the plugin schema (e.g. `?search_path=public_catalog`). |
| `token_secret` | no | HMAC secret for signing public catalog tokens. Auto-generated and persisted to `app_config` on first run if missing. Manifest-supplied values must be at least 32 chars. |
| `public_port` | no | Standalone public website port, for example `8090`. |
| `standalone_http_listen` | no | Advanced listener address, for example `127.0.0.1:8090`. Overrides `public_port`. |
| `public_base_url` | no | Absolute public URL used when generated links are returned. |
| `catalog_password` | no | Shared password required to browse `/catalog` unless a bypass token is used. Plaintext input — the plugin hashes this with bcrypt on first write and never persists the plaintext. Can be cleared / disabled at runtime from the admin page. |
| `ad_html` | no | Initial trusted operator HTML. The published HTML is edited from the plugin admin page after setup. |
| `ebook_installation_id` | no | Ebooks portal installation ID used for ebook stats and browsing. |
| `audiobook_installation_id` | no | Audiobooks portal installation ID used for audiobook stats and browsing. |

## Routes

| Route | Access | Purpose |
|---|---|---|
| `/` | public | Landing page. |
| `/api/public/stats` | public | Public catalog stats. |
| `/catalog` | public (password / bypass token / open mode) | Searchable public catalog view. |
| `/catalog?token=...` | public with bypass token | Searchable public catalog view without password prompt. |
| `/api/catalog/media` | public with password session or bypass token | Catalog data API. |
| `/api/public/catalog-login` | public | Validate catalog password and create a browser session cookie. |
| `/admin` | admin via Continuum | Operator admin page. |
| `/api/admin/catalog-token` | admin via Continuum | Generate permanent bypass links. |
| `/api/admin/catalog-links` | admin via Continuum | List / delete saved bypass links. |
| `/api/admin/html-section` | admin via Continuum | Read or publish landing-page HTML. |
| `/api/admin/config` | admin via Continuum | Read / patch the plugin config. |
| `/api/admin/catalog-password` | admin via Continuum | `DELETE` clears the stored hash and turns off password protection. |

## Database Setup

```sql
CREATE ROLE plugin_public_catalog WITH LOGIN PASSWORD '<chosen>';
CREATE SCHEMA public_catalog AUTHORIZATION plugin_public_catalog;
GRANT CONNECT ON DATABASE continuum TO plugin_public_catalog;

-- Required for catalog browsing, episode lookup, library counts, and
-- quality-bucket aggregation. The plugin reads these tables read-only
-- through the catalog_*.go files in internal/store.
GRANT USAGE ON SCHEMA public TO plugin_public_catalog;
GRANT SELECT ON public.media_items,
                 public.media_files,
                 public.media_folders,
                 public.media_item_libraries,
                 public.episodes,
                 public.seasons,
                 public.episode_libraries
   TO plugin_public_catalog;
```

Migrations are applied automatically on plugin start via `golang-migrate`;
the operator only needs to create the schema and grant the connect role.

## Catalog Password Management

The admin can manage the catalog gate in three orthogonal ways:

- **Set / change** — write a new value to `catalog_password` via the
  admin config endpoint. The plugin hashes with bcrypt and clears the
  plaintext on persist.
- **Toggle off** — set `catalog_password_required` to `false` in the
  config. The hash stays stored but the gate is disabled, so flipping
  it back on later restores the original password without re-entry.
- **Delete entirely** — `DELETE /api/admin/catalog-password`. Zeros the
  hash and forces the toggle off.

When the toggle is off **and** no hash is stored, the catalog is open
to anonymous visitors.

## Operations

- Prefer binding the standalone listener to loopback behind a reverse
  proxy.
- Use HTTPS for public access.
- Rotate `token_secret` to invalidate all outstanding bypass links and
  catalog login cookies. The auto-generated secret is stored in the DB,
  so rotation means setting a new manifest value or clearing the row.
- Keep published HTML simple and trusted; it is intentionally operator-
  provided HTML and is editable only from the plugin admin page.

## Known Limitations

- Catalog reads still touch `public.media_*` directly for episode-level
  browsing and quality buckets — see the Architecture section. When the
  host SDK exposes those queries, `internal/store/catalog_*.go` can be
  rewritten on top of the SDK and the cross-schema grants can be
  dropped.

## Web Stack

Frontend lives in `web/`. The Vite build emits into
`internal/server/public/dist` so the Go binary `//go:embed`s the SPA
straight from the build output. The stack matches the other
first-party plugins (`continuum-plugin-audiobooks`,
`continuum-plugin-ebooks`):

- React 19 + TypeScript + Vite + Vitest
- Tailwind CSS v4 with the shared design tokens
- radix-ui primitives, sonner toasts, lucide-react icons
- isomorphic-dompurify for sanitising operator-published HTML
- Package manager: pnpm (matches the workspace convention)

## Build And Test

```bash
make build
make test
```
