# Public Visitor Vs Admin Surfaces

A quick split of which behaviours are visitor-facing vs operator-only.
Useful when reasoning about access control, log redaction, or what the
SPA renders for a given user.

## Visitor surfaces

These run with no authentication, or with whatever scope a catalog
password or bypass token grants. They are reachable from the public
internet when the standalone listener is bound directly, or via the
host's public proxy when the routes are accessed through Continuum.

| URL | What the visitor sees |
| --- | --- |
| `GET /` | Landing page: library stats, operator-published HTML, link into the catalog. Scope-aware when a session cookie or `?token=` is present. |
| `GET /?page=<name>` | Saved-link variant of the landing page — substitutes the operator-published HTML for whatever HTML the link carries, and overrides the catalog button URL. |
| `GET /catalog` | Catalog browser. 401 (with the SPA shell still rendered) when no session/token and password is required. |
| `GET /catalog?token=...` | Catalog browser with a bypass token. Library/media-type scope is applied server-side. |
| `GET /item/{id}` | Item detail page (SPA). Session/token required. |
| `GET /assets/*` | Embedded SPA static assets. |
| `GET /api/public/stats` | Anonymous JSON stats. Always returns 200, even on failure. |
| `POST /api/public/catalog-login` | Issues the `public_catalog_auth` cookie when the bcrypt password matches. |
| `GET /api/catalog/media`, `/api/catalog/filters`, `/api/catalog/items/{id}`, `/api/catalog/items/{id}/seasons`, `/api/catalog/series/{id}/seasons/{season}/episodes` | Gated JSON API used by the SPA. 401 without a valid claim. |

What visitors **never** see:

- The token secret. Sessions and bypass tokens are signed but the
  signing key is never exposed.
- The bcrypt password hash. Even the admin UI only sees a "password is
  set" flag derived from a non-empty hash.
- Internal poster/backdrop paths. `overlayCatalogMediaImages` resolves
  raw TMDB/TVDB paths into host-served URLs before responses leave the
  plugin.
- Other libraries' items when a bypass token's `library_ids` scope is
  set — the intersect happens in `mergeLibraryIDs` and a request that
  asks for a forbidden library returns 400.

## Operator surfaces

All routes under `/api/admin/*` and `/admin` go through `requireAdmin`,
which gates on host-stamped identity headers. The host strips inbound
`X-Continuum-*` identity headers from public requests before forwarding,
so seeing them in the plugin is a trustworthy signal.

| URL | Method | Use |
| --- | --- | --- |
| `/admin` | GET | Admin SPA. Visible in Continuum's admin nav as "Public Catalog". |
| `/api/admin/config` | GET / PATCH | Read or update plugin config. Secrets are redacted on read; live listener changes are rejected on write. |
| `/api/admin/catalog-password` | DELETE | Clear the catalog password and force the gate off. |
| `/api/admin/html-section` | GET / PUT | Read or replace the landing-page HTML block. 200 KB cap on writes. |
| `/api/admin/catalog-token` | POST | Mint a bypass token. Optionally persists it as a saved link. |
| `/api/admin/catalog-links` | GET | List saved links. |
| `/api/admin/catalog-links/{id}` | DELETE | Delete a saved link. Does NOT revoke the underlying token — rotate `token_secret` for that. |

Operator workflows mapped to surfaces:

| Task | Surface |
| --- | --- |
| Publish a landing message | `PUT /api/admin/html-section` from the admin SPA's HTML editor. |
| Enable / disable the password gate | `PATCH /api/admin/config` with `catalog_password_required`. |
| Set a new password | `PATCH /api/admin/config` with `catalog_password` plaintext (hashed on write). |
| Clear the password entirely | `DELETE /api/admin/catalog-password`. |
| Generate a per-partner bypass link | `POST /api/admin/catalog-token` with `libraryIds`, `mediaTypes`, `saveName`, and optional per-link `html`. |
| Revoke a specific bypass link | Best you can do is `DELETE /api/admin/catalog-links/{id}` (operator UX); the token itself remains valid until you rotate `token_secret`. |
| Update the host this plugin advertises | `PATCH /api/admin/config` with `public_base_url`. Newly-minted bypass links carry the new host; old saved-link rows keep their original `url`. |
| Federate ebook/audiobook stats | `PATCH /api/admin/config` with `ebook_installation_id` / `audiobook_installation_id`. |

## Trust boundary summary

```
internet ──HTTP──▶ standalone listener (optional)
                      │
                      ▼
                   chi router  ◀── host SDK proxy via /public-catalog/*
                      │
        ┌─────────────┼─────────────┬──────────────────┐
        ▼             ▼             ▼                  ▼
   public routes  catalog API   admin routes      assets
   (no gate)      (claim req'd) (requireAdmin)   (no gate)
                                      │
                                      └── requires host-stamped
                                          X-Continuum-User-Id and
                                          X-Continuum-User-Role=admin
                                          headers (set by Continuum
                                          on authenticated requests
                                          only)
```

The operator's choices for keeping the trust boundary intact:

- Run the standalone listener on **loopback only** (`127.0.0.1:8090`)
  and front it with a reverse proxy that does TLS, rate-limiting, and
  any further IP allowlisting.
- Use the Continuum host's existing proxy for the routes when you don't
  need a separate public hostname — that automatically inherits the
  host's CORS/CSRF/TLS posture.
- Set `public_base_url` to the externally-resolvable URL so generated
  bypass links work for external recipients.
- Rotate `token_secret` if you suspect a leak; that invalidates every
  outstanding cookie and bypass link in one step.
