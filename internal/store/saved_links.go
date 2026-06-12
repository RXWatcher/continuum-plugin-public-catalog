package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// generateLinkSlug returns an unguessable, URL-safe slug for a saved
// link. 16 random bytes (128 bits) hex-encoded is collision-resistant
// and not enumerable.
func generateLinkSlug() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate link slug: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// SaveCatalogLink upserts a named bypass link by `name`. A stable,
// unguessable slug is assigned on first insert and preserved across
// updates (so a re-saved link keeps the same public ?page=<slug> URL).
// Returns the persisted row (with timestamps populated).
func (s *Store) SaveCatalogLink(ctx context.Context, link SavedCatalogLink) (SavedCatalogLink, error) {
	link.Name = strings.TrimSpace(link.Name)
	if link.Name == "" {
		return SavedCatalogLink{}, fmt.Errorf("link name is required")
	}
	slug, err := generateLinkSlug()
	if err != nil {
		return SavedCatalogLink{}, err
	}
	// On conflict (re-save of an existing name) keep the existing slug so
	// previously-shared landing URLs don't break.
	err = s.pool.QueryRow(ctx, `
		INSERT INTO saved_catalog_links (name, slug, token, url, html, media_types, library_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (name) DO UPDATE SET
		  token = EXCLUDED.token,
		  url = EXCLUDED.url,
		  html = EXCLUDED.html,
		  media_types = EXCLUDED.media_types,
		  library_ids = EXCLUDED.library_ids,
		  updated_at = NOW()
		RETURNING id, name, slug, token, url, html, media_types, library_ids, created_at, updated_at
	`, link.Name, slug, link.Token, link.URL, link.HTML, link.MediaTypes, link.LibraryIDs).Scan(
		&link.ID, &link.Name, &link.Slug, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt,
	)
	if err != nil {
		return SavedCatalogLink{}, fmt.Errorf("save catalog link: %w", err)
	}
	return link, nil
}

// ListCatalogLinks returns all saved links ordered by most-recently-updated.
func (s *Store) ListCatalogLinks(ctx context.Context) ([]SavedCatalogLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, slug, token, url, html, media_types, library_ids, created_at, updated_at
		FROM saved_catalog_links
		ORDER BY updated_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list catalog links: %w", err)
	}
	defer rows.Close()
	out := []SavedCatalogLink{}
	for rows.Next() {
		var link SavedCatalogLink
		if err := rows.Scan(&link.ID, &link.Name, &link.Slug, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan catalog link: %w", err)
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog links: %w", err)
	}
	return out, nil
}

// GetCatalogLinkBySlug looks up a single saved link by its unguessable
// slug. Resolving by slug (instead of the guessable display name) is
// what keeps the embedded bypass token from leaking to anyone who can
// guess a link's name. Returns (zero, false, nil) when no link matches —
// a separate signal from a real DB error.
func (s *Store) GetCatalogLinkBySlug(ctx context.Context, slug string) (SavedCatalogLink, bool, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return SavedCatalogLink{}, false, nil
	}
	var link SavedCatalogLink
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, token, url, html, media_types, library_ids, created_at, updated_at
		FROM saved_catalog_links
		WHERE slug = $1
	`, slug).Scan(&link.ID, &link.Name, &link.Slug, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SavedCatalogLink{}, false, nil
	}
	if err != nil {
		return SavedCatalogLink{}, false, fmt.Errorf("get catalog link: %w", err)
	}
	return link, true, nil
}

// DeleteCatalogLink removes one saved link by id. Returns ErrNotFound
// if the id didn't exist.
func (s *Store) DeleteCatalogLink(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM saved_catalog_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete catalog link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
