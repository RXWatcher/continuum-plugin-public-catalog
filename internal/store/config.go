package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"

	pluginrt "github.com/RXWatcher/silo-plugin-public-catalog/internal/runtime"
)

// GetConfig reads the singleton app_config row, decodes the JSONB payload
// into a Config, and runs in-code normalisation on the result. Creates
// the row with defaults if it's missing.
func (s *Store) GetConfig(ctx context.Context) (pluginrt.Config, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM app_config WHERE id = 1`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING`); err != nil {
			return pluginrt.Config{}, fmt.Errorf("ensure app_config: %w", err)
		}
		return s.GetConfig(ctx)
	}
	if err != nil {
		return pluginrt.Config{}, fmt.Errorf("get app_config: %w", err)
	}
	cfg := pluginrt.DefaultAppConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return pluginrt.Config{}, fmt.Errorf("decode app_config: %w", err)
		}
	}
	return pluginrt.NormalizeAppConfig(cfg)
}

// UpdateConfig validates and persists the supplied Config. If a plaintext
// CatalogPassword is set on the input it is hashed and moved into
// CatalogPasswordHash before storage — plaintext never survives a write.
// DatabaseURL is intentionally stripped (it's runtime-only).
func (s *Store) UpdateConfig(ctx context.Context, cfg pluginrt.Config) error {
	cfg, err := pluginrt.NormalizeAppConfig(cfg)
	if err != nil {
		return err
	}
	cfg.DatabaseURL = ""

	if cfg.CatalogPassword != "" {
		hash, err := HashCatalogPassword(cfg.CatalogPassword)
		if err != nil {
			return err
		}
		cfg.CatalogPasswordHash = hash
		cfg.CatalogPassword = ""
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode app_config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_config (id, data, updated_at) VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()
	`, raw)
	if err != nil {
		return fmt.Errorf("update app_config: %w", err)
	}
	return nil
}

// Bootstrap is called by main.go once per plugin start. It merges the
// manifest-supplied legacy config into the DB row (only if the DB still
// holds defaults), then ensures TokenSecret and CatalogPasswordRequired
// are set to sensible values. Returns the canonical Config used by the
// rest of the plugin.
//
// This consolidates several pieces of formerly scattered persistence:
//   - Auto-generated token secret used to live in a disk file next to
//     the binary; now stored in app_config so it survives reinstall.
//   - Operator-edited landing HTML used to live in another disk file;
//     now lives in Config.PublishedHTML.
//   - Legacy plaintext catalog password is hashed on first write.
func (s *Store) Bootstrap(ctx context.Context, manifest pluginrt.Config) (pluginrt.Config, error) {
	cur, err := s.GetConfig(ctx)
	if err != nil {
		return pluginrt.Config{}, err
	}
	dirty := false
	defaultCfg, err := pluginrt.NormalizeAppConfig(pluginrt.DefaultAppConfig())
	if err != nil {
		return pluginrt.Config{}, err
	}

	// First-run seed: only import manifest values when the DB is still
	// at defaults. After any operator edit, manifest values are ignored.
	if reflect.DeepEqual(cur, defaultCfg) {
		manifest.DatabaseURL = ""
		if manifest.CatalogPassword != "" {
			hash, err := HashCatalogPassword(manifest.CatalogPassword)
			if err != nil {
				return pluginrt.Config{}, err
			}
			manifest.CatalogPasswordHash = hash
			manifest.CatalogPasswordRequired = true
			manifest.CatalogPassword = ""
		}
		if !reflect.DeepEqual(manifest, cur) {
			cur = manifest
			dirty = true
		}
	}

	// Auto-generate a token secret if none exists. Persists immediately
	// so reinstall doesn't regenerate.
	if cur.TokenSecret == "" {
		secret, err := generateTokenSecret()
		if err != nil {
			return pluginrt.Config{}, err
		}
		cur.TokenSecret = secret
		cur.TokenSecretGenerated = true
		dirty = true
	}

	// Legacy plaintext password upgrade: hash it on first run.
	if cur.CatalogPassword != "" {
		hash, err := HashCatalogPassword(cur.CatalogPassword)
		if err != nil {
			return pluginrt.Config{}, err
		}
		cur.CatalogPasswordHash = hash
		cur.CatalogPasswordRequired = true
		cur.CatalogPassword = ""
		dirty = true
	}

	if dirty {
		if err := s.UpdateConfig(ctx, cur); err != nil {
			return pluginrt.Config{}, err
		}
		cur, err = s.GetConfig(ctx)
		if err != nil {
			return pluginrt.Config{}, err
		}
	}
	return cur, nil
}

// ClearCatalogPassword removes the stored hash and forces password
// protection off. Idempotent.
func (s *Store) ClearCatalogPassword(ctx context.Context) error {
	cur, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	cur.CatalogPasswordHash = ""
	cur.CatalogPassword = ""
	cur.CatalogPasswordRequired = false
	return s.UpdateConfig(ctx, cur)
}

// RehashCatalogPassword is called by the login handler after a legacy
// plaintext match, to upgrade the storage to bcrypt without forcing the
// admin to re-enter the password.
func (s *Store) RehashCatalogPassword(ctx context.Context, plaintext string) error {
	cur, err := s.GetConfig(ctx)
	if err != nil {
		return err
	}
	hash, err := HashCatalogPassword(plaintext)
	if err != nil {
		return err
	}
	cur.CatalogPasswordHash = hash
	cur.CatalogPassword = ""
	return s.UpdateConfig(ctx, cur)
}

func generateTokenSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token_secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
