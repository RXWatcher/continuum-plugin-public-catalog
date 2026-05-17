# Public Catalog Setup, Debugging, And Flows

Plugin ID: `continuum.public-catalog`
Version documented: `0.1.0`

## Purpose

public advertising/catalog surface with signed-token browsing for selected ebook and
audiobook portals.

## Runtime Dependencies

- Continuum plugin host
- Configured token_secret and public_base_url
- Optional continuum.ebooks installation
- Optional continuum.audiobooks installation

## Setup Checklist

1. Configure token_secret with a strong random secret.
2. Set public_base_url to the public URL.
3. Optionally set ebook_installation_id and audiobook_installation_id.
4. Add ad_html if the landing page should include custom marketing copy.
5. Open /catalog publicly and test signed browsing links.

## Configuration Reference

- `token_secret`
- `standalone_http_listen`
- `public_base_url`
- `ad_html`
- `token_ttl_hours`
- `ebook_installation_id`
- `audiobook_installation_id`

Use the plugin manifest/admin form as the source of truth for field validation and defaults. Keep database credentials scoped to the plugin schema unless a plugin explicitly needs read access to Continuum core tables.

## Exposed Routes

- `GET / [public]`
- `GET /catalog [public]`
- `* /api/public/* [public]`
- `* /api/catalog/* [public]`
- `* /api/admin/* [admin]`
- `GET /admin [admin]`

## Capabilities

- `http_routes.v1 (public-catalog) - Public advertising page with stats, embedded HTML, and signed-token catalog browsing.`

## Operational Flows

### Public browse

1. Visitor opens the public catalog page.
2. The plugin renders configured public/ad content.
3. For protected catalog browsing, it mints or validates short-lived signed tokens.
4. It calls the selected Ebooks/Audiobooks installation APIs for safe catalog data.

## How This Plugin Communicates

- Can call Ebooks and Audiobooks portal APIs by installation ID.
- Serves public routes without normal Continuum login.
- Uses signed tokens to avoid exposing broad authenticated APIs.

## Debugging Runbook

- If public links fail, verify public_base_url and reverse proxy route forwarding.
- If catalog sections are empty, confirm installation IDs and that the target portal has visible libraries.
- If tokens are rejected after restart, confirm token_secret did not change.
- Keep token_ttl_hours short enough for public use.

## Log And Health Checks

- Start with Continuum Admin -> Plugins and confirm the installation is enabled.
- Check the plugin process logs around startup for manifest loading, migration, and route registration.
- Check scheduled task logs when a workflow depends on polling or reconciliation.
- Confirm the plugin routes are reachable through Continuum using the access level shown above.
- For database-backed plugins, verify the configured role can connect, create/migrate tables in its schema, and read/write expected rows.

## Common Failure Patterns

- Wrong installation ID selected in a portal or router setting after reinstalling a plugin.
- Plugin database URL points at the public schema instead of the dedicated plugin schema.
- Reverse proxy forwards the SPA route but not `/api/*`, `/api/v1/*`, `/assets/*`, or provider-specific public routes.
- Network checks are run from the operator laptop instead of from the Continuum/plugin runtime network.
- Secrets are regenerated during restart, invalidating signed URLs, encrypted fields, or login state.

## Verification After Changes

1. Restart or reload the plugin installation.
2. Open the plugin route or admin page in Continuum.
3. Exercise the smallest workflow that crosses a plugin boundary.
4. Confirm both the source plugin and destination plugin record the same request/session/login identifier.
5. Leave the scheduled reconciler enough time to run, then confirm terminal state or a useful error.
