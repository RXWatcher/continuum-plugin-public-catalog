DROP INDEX IF EXISTS saved_catalog_links_slug_key;
ALTER TABLE saved_catalog_links DROP COLUMN IF EXISTS slug;
