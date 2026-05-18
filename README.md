# Public Catalog for Continuum

`continuum.public-catalog` publishes a small public-facing website for a
Continuum deployment. It shows public stats, renders admin-published HTML, and
protects the searchable catalog behind a shared password unless a permanent
bypass link is used.

The plugin reads catalog information through Continuum host and plugin APIs. It
does not read Arr databases, local files, or Continuum's database tables
directly.

## Detailed Operations Docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)

## Features

- Standalone public HTTP listener with simple public port configuration.
- Public landing page with stats and admin-published trusted HTML.
- Public stats sourced from configured Ebooks and Audiobooks portal
  installations.
- Password-protected catalog browsing.
- Admin endpoint for generating permanent catalog bypass links.
- Admin HTML section editor for the public landing page.
- Inbound public requests strip Continuum identity headers before routing.

## Configuration

| Key | Required | Description |
|---|---|---|
| `token_secret` | yes | HMAC secret for signing public catalog tokens. Use at least 32 random bytes. |
| `public_port` | no | Standalone public website port, for example `8090`. |
| `standalone_http_listen` | no | Advanced listener address, for example `127.0.0.1:8090`. Overrides `public_port`. |
| `public_base_url` | no | Absolute public URL used when generated links are returned. |
| `catalog_password` | yes | Shared password required to browse `/catalog` unless a bypass token is used. |
| `ad_html` | no | Initial trusted operator HTML. The published HTML is edited from the plugin admin page after setup. |
| `ebook_installation_id` | no | Ebooks portal installation ID used for ebook stats and browsing. |
| `audiobook_installation_id` | no | Audiobooks portal installation ID used for audiobook stats and browsing. |

## Routes

| Route | Access | Purpose |
|---|---|---|
| `/` | public | Landing page. |
| `/api/public/stats` | public | Public catalog stats. |
| `/catalog` | public with password | Searchable public catalog view. |
| `/catalog?token=...` | public with bypass token | Searchable public catalog view without password prompt. |
| `/api/catalog/media` | public with password session or bypass token | Catalog data API. |
| `/api/public/catalog-login` | public | Validate catalog password and create a browser session cookie. |
| `/api/admin/catalog-token` | admin via Continuum | Generate permanent bypass links. |
| `/api/admin/html-section` | admin via Continuum | Read or publish landing-page HTML. |

## Operations

- Prefer binding the standalone listener to loopback behind a reverse proxy.
- Use HTTPS for public access.
- Rotate `token_secret` to invalidate all outstanding bypass links and catalog
  login cookies.
- Keep published HTML simple and trusted; it is intentionally operator-provided
  HTML and is editable only from the plugin admin page.

## Build And Test

```bash
make build
make test
```
