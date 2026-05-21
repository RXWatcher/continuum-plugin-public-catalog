# Tokens And Auth

The plugin has exactly one auth primitive: an HMAC-SHA256 signed JSON
claim string. Both the catalog session cookie and the per-recipient
bypass link are the same shape — the only difference is where the
client carries it and what scope is baked in.

## Token shape

Serialised form: `<base64url(claims)>.<base64url(hmac-sha256(claims))>`.
There is no header (it's not a JWT). The HMAC is over the raw base64url
payload, not the decoded JSON.

```go
type tokenClaims struct {
    Scope      string   `json:"scope"`        // always "catalog"
    ExpiresAt  int64    `json:"exp"`          // unix seconds, 0 = no expiry
    LibraryIDs []string `json:"library_ids,omitempty"`
    MediaTypes []string `json:"media_types,omitempty"`
}
```

See `internal/server/token.go::signToken` / `verifyToken`.

`verifyToken` checks signature, decodes the payload, verifies
`scope == "catalog"`, and enforces `exp` (when non-zero). The signing
secret comes from `Deps.TokenSecret` which is loaded from the singleton
`app_config.token_secret`.

## Session cookie (password path)

`POST /api/public/catalog-login` issues a `public_catalog_auth` cookie
when the supplied password matches the stored hash. The cookie value is
a token with `Scope: "catalog"` and **no other claims** — it inherits
the full library catalog scope.

Cookie attributes (`handlers_auth.go::issueCatalogSession`):

- `HttpOnly` — JS in the SPA can't read it.
- `SameSite=Lax` — first-party only; the bypass-link flow doesn't use
  cookies.
- `Secure` — set whenever `r.TLS != nil`. Behind a TLS-terminating
  reverse proxy you'll want either to forward the `X-Forwarded-Proto`
  header AND configure your proxy to set `Secure`-conscious cookies,
  or to terminate TLS directly on the plugin (rare).
- `Path=/` — covers `/catalog`, `/item`, and the catalog API.

The cookie has no explicit expiry, so it lasts until the browser
forgets it OR until the token's `ExpiresAt` triggers (currently
unspecified — session cookies are signed with `exp: 0`, so they only
expire when the secret rotates).

## Bypass token (link path)

`POST /api/admin/catalog-token` mints a token with whatever scope the
admin requests:

```go
tokenClaims{
    Scope:      "catalog",
    LibraryIDs: cleanList(req.LibraryIDs),
    MediaTypes: cleanList(req.MediaTypes),
}
```

(`handlers_admin_api.go::hCreateToken`). The admin endpoint currently
does **not** set an expiry on minted tokens, even though the runtime
config holds `TokenTTLHours`. The TTL is reserved for a future field on
the create-token payload; right now bypass tokens last until the
secret rotates.

The client receives `{token, url}`. The `url` is built from
`publicBaseURLForRequest` (see [request-flows.md](request-flows.md) and
`handlers_landing.go::publicBaseURLForRequest`):

1. `public_base_url` config → use it.
2. Else, non-wildcard `standalone_http_listen` + scheme guess → synthesise.
3. Else (wildcard listener + no base URL) → return the relative path
   `catalog?token=...` and let the SPA resolve against
   `window.location.origin`.

When `saveName` is set, the link is also persisted to
`saved_catalog_links`. A persisted link can carry its own HTML block
(`saved_catalog_links.html`), surfaced by `?page=<name>` on the landing
route. Deleting a saved link revokes the link from the operator's view
but does NOT invalidate the underlying token — the HMAC signature is
still valid until the secret rotates. If you need to revoke individual
links, rotate the secret and re-mint the keepers.

## Scope intersection

The token's `LibraryIDs` and `MediaTypes` are the OUTER bound. Inside
that, the request's own query params can scope further. See
`internal/server/validation.go::mergeLibraryIDs` /
`mergeMediaTypes`:

- Empty allowed → request wins outright.
- Empty requested → allowed wins outright.
- Both set → intersect; empty intersection returns
  `(_, false)` so the caller can emit a 400.

`hCatalogMedia` returns:

- 400 `invalid_media_type` when the request asks for a media type the
  token doesn't allow.
- 400 `invalid_library` when the request asks for a library the token
  doesn't allow.

This is intentional — silently dropping a forbidden filter would let a
recipient browse libraries their link wasn't meant to cover.

## Password storage

`internal/store/password.go::HashCatalogPassword` wraps
`bcrypt.GenerateFromPassword` with `bcrypt.DefaultCost`. Verification
recognises both bcrypt (`$2`-prefix) and a legacy plaintext field
(`Config.CatalogPassword`), comparing the latter in constant time
(`subtle.ConstantTimeCompare`).

After a legacy match, `hCatalogLogin` kicks off a 5-second-bounded
background rehash so a slow bcrypt write doesn't block the response.
A failure logs a `password rehash failed` warning but does not affect
the just-issued session.

## What rotation invalidates

| Rotation | Sessions | Saved links | Pending tokens | Passwords |
| --- | --- | --- | --- | --- |
| `token_secret` change | All cookies fail verification → users prompted again. | All saved-link tokens fail verification → re-mint required. | All in-flight tokens fail. | Untouched. |
| Password change | Untouched (cookies are signed with `token_secret`, not the password). | Untouched. | Untouched. | Old hash overwritten. |
| `DELETE /api/admin/catalog-password` | Untouched, but `CatalogPasswordRequired=false` makes them moot. | Untouched. | Untouched. | Hash cleared. |

Rotating the token secret is the **only** way to revoke an outstanding
bypass link without deleting the entire schema. Plan accordingly.
