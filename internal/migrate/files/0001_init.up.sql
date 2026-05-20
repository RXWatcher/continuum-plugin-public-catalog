-- Singleton row holding all operator-tunable plugin config as JSONB.
-- One row keeps schema evolution to in-app code (Config struct) and
-- avoids a migration per field addition. The published HTML section,
-- catalog password hash, and auto-generated token secret all live here.
CREATE TABLE IF NOT EXISTS app_config (
    id          INTEGER PRIMARY KEY DEFAULT 1,
    data        JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT app_config_singleton CHECK (id = 1)
);
INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING;

-- Saved bypass links. Each row is a named, signed catalog-scope token
-- that operators can hand out (e.g. for trusted partners) without making
-- them remember the catalog password.
CREATE TABLE IF NOT EXISTS saved_catalog_links (
    id           BIGSERIAL PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    token        TEXT NOT NULL,
    url          TEXT NOT NULL,
    html         TEXT NOT NULL DEFAULT '',
    media_types  TEXT[] NOT NULL DEFAULT '{}'::text[],
    library_ids  TEXT[] NOT NULL DEFAULT '{}'::text[],
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Cache of host-provided provider image URLs so we don't re-resolve the
-- same poster/backdrop on every catalog page render.
CREATE TABLE IF NOT EXISTS provider_image_cache (
    cache_key   TEXT PRIMARY KEY,
    url         TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS provider_image_cache_expires_idx ON provider_image_cache (expires_at);
