package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SaveCatalogLink upserts a named bypass link by `name`. Returns the
// persisted row (with timestamps populated).
func (s *Store) SaveCatalogLink(ctx context.Context, link SavedCatalogLink) (SavedCatalogLink, error) {
	link.Name = strings.TrimSpace(link.Name)
	if link.Name == "" {
		return SavedCatalogLink{}, fmt.Errorf("link name is required")
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO saved_catalog_links (name, token, url, html, media_types, library_ids)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET
		  token = EXCLUDED.token,
		  url = EXCLUDED.url,
		  html = EXCLUDED.html,
		  media_types = EXCLUDED.media_types,
		  library_ids = EXCLUDED.library_ids,
		  updated_at = NOW()
		RETURNING id, name, token, url, html, media_types, library_ids, created_at, updated_at
	`, link.Name, link.Token, link.URL, link.HTML, link.MediaTypes, link.LibraryIDs).Scan(
		&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt,
	)
	if err != nil {
		return SavedCatalogLink{}, fmt.Errorf("save catalog link: %w", err)
	}
	return link, nil
}

// ListCatalogLinks returns all saved links ordered by most-recently-updated.
func (s *Store) ListCatalogLinks(ctx context.Context) ([]SavedCatalogLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, token, url, html, media_types, library_ids, created_at, updated_at
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
		if err := rows.Scan(&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan catalog link: %w", err)
		}
		out = append(out, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate catalog links: %w", err)
	}
	return out, nil
}

// GetCatalogLinkByName looks up a single saved link. Returns
// (zero, false, nil) if no link matches the given (case-insensitive)
// name — separate signal from a real DB error.
func (s *Store) GetCatalogLinkByName(ctx context.Context, name string) (SavedCatalogLink, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SavedCatalogLink{}, false, nil
	}
	var link SavedCatalogLink
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, token, url, html, media_types, library_ids, created_at, updated_at
		FROM saved_catalog_links
		WHERE lower(name) = lower($1)
	`, name).Scan(&link.ID, &link.Name, &link.Token, &link.URL, &link.HTML, &link.MediaTypes, &link.LibraryIDs, &link.CreatedAt, &link.UpdatedAt)
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
