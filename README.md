# continuum-plugin-public-catalog

Public advertising catalog for Continuum.

The plugin exposes a standalone public HTTP listener with:

- public stats sourced from Continuum's catalog
- operator-configured embedded HTML
- a signed-token catalog link for searchable/filterable public browsing

The catalog is read through the Continuum RuntimeHost catalog APIs. It does not
read ArrProxyDB, local files, or Continuum's database directly.

## Configuration

| Field | Required | Description |
|---|---:|---|
| `token_secret` | yes | HMAC secret used to sign catalog access links. |
| `standalone_http_listen` | no | Address for the public listener, e.g. `127.0.0.1:8090`. |
| `public_base_url` | no | Absolute public URL used when generating links. |
| `ad_html` | no | HTML block rendered on the public landing page. |
| `token_ttl_hours` | no | Default generated catalog-link lifetime. Defaults to 168. |

## Routes

- `GET /` public page
- `GET /api/public/stats`
- `GET /catalog?token=...`
- `GET /api/catalog/media?token=...`
- `POST /api/admin/catalog-token` admin-only token generation through the Continuum plugin proxy

The standalone listener strips inbound `X-Continuum-*` headers before routing,
so direct public requests cannot forge host identity.
