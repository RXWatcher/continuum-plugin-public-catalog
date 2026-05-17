# Public Catalog for Continuum

`continuum.public-catalog` publishes a small public-facing catalog and landing
page for a Continuum deployment. It can show public stats, render
operator-configured HTML, and issue signed catalog links for limited public
browsing.

The plugin reads catalog information through Continuum host and plugin APIs. It
does not read Arr databases, local files, or Continuum's database tables
directly.

## Detailed Operations Docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Features

- Standalone public HTTP listener.
- Public landing page with optional embedded operator HTML.
- Public stats sourced from configured Ebooks and Audiobooks portal
  installations.
- Signed-token catalog browsing.
- Admin endpoint for generating catalog links.
- Inbound public requests strip Continuum identity headers before routing.

## Configuration

| Key | Required | Description |
|---|---|---|
| `token_secret` | yes | HMAC secret for signing public catalog tokens. Use at least 32 random bytes. |
| `standalone_http_listen` | no | Public listener address, for example `127.0.0.1:8090` or `:8090`. |
| `public_base_url` | no | Absolute public URL used when generated links are returned. |
| `ad_html` | no | HTML rendered on the public landing page. |
| `token_ttl_hours` | no | Default generated catalog link lifetime. Defaults to 168 hours. |
| `ebook_installation_id` | no | Ebooks portal installation ID used for ebook stats and browsing. |
| `audiobook_installation_id` | no | Audiobooks portal installation ID used for audiobook stats and browsing. |

## Routes

| Route | Access | Purpose |
|---|---|---|
| `/` | public | Landing page. |
| `/api/public/stats` | public | Public catalog stats. |
| `/catalog?token=...` | public with token | Searchable public catalog view. |
| `/api/catalog/media?token=...` | public with token | Catalog data API. |
| `/api/admin/catalog-token` | admin via Continuum | Generate signed catalog links. |

## Operations

- Prefer binding the standalone listener to loopback behind a reverse proxy.
- Use HTTPS for public access.
- Rotate `token_secret` to invalidate all outstanding catalog links.
- Keep `ad_html` simple and trusted; it is intentionally operator-provided
  HTML.

## Build And Test

```bash
make build
make test
```
