-- Add an unguessable slug to saved bypass links. The landing page used
-- to resolve ?page=<display name>, which is guessable, and handed back a
-- URL containing the link's bypass token. Resolving only by this random
-- slug closes that token-leak vector while keeping the display name for
-- the admin UI.
ALTER TABLE saved_catalog_links
    ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';

-- Backfill existing rows with a random slug so older links resolve via a
-- non-guessable token too. md5(random()||clock_timestamp()) needs no
-- extension and is unique enough for a backfill; new rows get a
-- crypto/rand slug from the application layer.
UPDATE saved_catalog_links
SET slug = md5(random()::text || clock_timestamp()::text || id::text)
WHERE slug = '';

CREATE UNIQUE INDEX IF NOT EXISTS saved_catalog_links_slug_key
    ON saved_catalog_links (slug);
