// Package runtime is the SDK-facing plugin runtime: it owns the GetManifest
// / Configure RPC, accepts the manifest-supplied config bag, validates it,
// and hands a normalized Config struct to main.go's onConfig callback.
//
// The Config struct is also the persisted shape — store/config.go serialises
// it as the singleton app_config jsonb row, so adding a field here adds it
// to the persistent shape automatically.
package runtime

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// Config is the union of manifest-supplied and DB-persisted plugin settings.
// DatabaseURL is manifest-only (we can't talk to Postgres until we know its
// DSN). Everything else round-trips through app_config.data as JSONB.
type Config struct {
	DatabaseURL             string `json:"-"`
	TokenSecret             string `json:"token_secret,omitempty"`
	TokenSecretGenerated    bool   `json:"token_secret_generated,omitempty"`
	StandaloneHTTPListen    string `json:"standalone_http_listen,omitempty"`
	PublicPort              int    `json:"public_port,omitempty"`
	PublicBaseURL           string `json:"public_base_url,omitempty"`
	AdHTML                  string `json:"ad_html,omitempty"`
	PublishedHTML           string `json:"published_html,omitempty"`
	// CatalogPassword carries an operator-supplied plaintext password from
	// the manifest config or the admin UI. It is never persisted as
	// plaintext: store.Bootstrap and store.UpdateConfig both hash it into
	// CatalogPasswordHash on write and clear this field.
	CatalogPassword string `json:"catalog_password,omitempty"`
	// CatalogPasswordHash is the bcrypt hash of the catalog password.
	// Empty when no password has been set.
	CatalogPasswordHash string `json:"catalog_password_hash,omitempty"`
	// CatalogPasswordRequired controls whether browsing /catalog needs
	// the password (or a bypass token). When false the catalog is open
	// to anyone, regardless of whether a hash is stored. Letting an
	// operator toggle this without losing the stored hash means they can
	// temporarily open the catalog and lock it back down later.
	CatalogPasswordRequired bool   `json:"catalog_password_required"`
	TokenTTLHours           int    `json:"token_ttl_hours,omitempty"`
	EbookInstallationID     string `json:"ebook_installation_id,omitempty"`
	AudioInstallationID     string `json:"audio_installation_id,omitempty"`
}

type Server struct {
	runtimedefault.Server
	manifest *pluginv1.PluginManifest
	onConfig func(Config) error

	mu  sync.RWMutex
	cfg Config
}

func New(manifest *pluginv1.PluginManifest, onConfig func(Config) error) *Server {
	return &Server{manifest: manifest, onConfig: onConfig}
}

func (s *Server) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

// DefaultAppConfig returns the in-code defaults applied when no DB row
// exists yet.
func DefaultAppConfig() Config {
	return Config{TokenTTLHours: 168}
}

// NormalizeAppConfig validates and canonicalises a Config. It does NOT
// generate secrets — that's the store's job during Bootstrap, so the
// persistence layer is the single source of truth for derived state.
func NormalizeAppConfig(cfg Config) (Config, error) {
	if cfg.TokenSecret != "" && len(cfg.TokenSecret) < 32 {
		return Config{}, fmt.Errorf("token_secret must be at least 32 characters")
	}
	if cfg.PublicBaseURL != "" {
		u, err := url.Parse(cfg.PublicBaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return Config{}, fmt.Errorf("public_base_url must be an absolute URL")
		}
	}
	if cfg.PublicPort < 0 || cfg.PublicPort > 65535 {
		return Config{}, fmt.Errorf("public_port must be between 1 and 65535")
	}
	if cfg.PublicPort > 0 && cfg.StandaloneHTTPListen == "" {
		cfg.StandaloneHTTPListen = fmt.Sprintf(":%d", cfg.PublicPort)
	}
	// Pre-validate the resolved listener address so a typo (`127001:8090`,
	// `:99999`, missing port) fails the Configure RPC cleanly instead of
	// surfacing async in a goroutine log a few seconds after start.
	if cfg.StandaloneHTTPListen != "" {
		if err := validateListenAddr(cfg.StandaloneHTTPListen); err != nil {
			return Config{}, fmt.Errorf("standalone_http_listen %q is invalid: %w", cfg.StandaloneHTTPListen, err)
		}
	}
	if cfg.TokenTTLHours < 1 {
		cfg.TokenTTLHours = 168
	}
	return cfg, nil
}

// validateListenAddr catches syntactic problems with the configured
// listen address (`:8090`, `host:8090`, `[::1]:8090`) without doing
// DNS or trying to bind. A bad hostname that resolves at bind time
// still gets caught synchronously in main.go's net.Listen call; this
// just rejects shapes that can't possibly bind.
func validateListenAddr(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("address is empty")
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must include host:port (use `:8090` for any host)")
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		return fmt.Errorf("port %q invalid: %w", portStr, err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range", port)
	}
	return nil
}

// IsWildcardListen reports whether the configured listen address binds
// to every interface (the `:port` shorthand or an explicit `0.0.0.0` /
// `[::]`). main.go warns on this — the README recommends loopback.
func IsWildcardListen(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "" || host == "0.0.0.0" || host == "::"
}

func (s *Server) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	cfg := DefaultAppConfig()
	for _, e := range req.GetConfig() {
		if e.GetValue() == nil {
			continue
		}
		m := e.GetValue().AsMap()
		switch e.GetKey() {
		case "database_url":
			cfg.DatabaseURL = stringValue(m["value"], firstString(m))
		case "token_secret":
			cfg.TokenSecret = stringValue(m["value"], firstString(m))
		case "standalone_http_listen":
			cfg.StandaloneHTTPListen = stringValue(m["value"], firstString(m))
		case "public_port":
			cfg.PublicPort = intValue(m["value"], firstNumber(m), cfg.PublicPort)
		case "public_base_url":
			cfg.PublicBaseURL = stringValue(m["value"], firstString(m))
		case "ad_html":
			cfg.AdHTML = stringValue(m["value"], firstString(m))
		case "catalog_password":
			cfg.CatalogPassword = stringValue(m["value"], firstString(m))
		case "token_ttl_hours":
			cfg.TokenTTLHours = intValue(m["value"], firstNumber(m), cfg.TokenTTLHours)
		case "ebook_installation_id":
			cfg.EbookInstallationID = stringValue(m["value"], firstString(m))
		case "audiobook_installation_id":
			cfg.AudioInstallationID = stringValue(m["value"], firstString(m))
		}
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database_url is required")
	}
	var err error
	cfg, err = NormalizeAppConfig(cfg)
	if err != nil {
		return nil, err
	}
	if s.onConfig != nil {
		if err := s.onConfig(cfg); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return &pluginv1.ConfigureResponse{}, nil
}

func stringValue(candidates ...any) string {
	for _, c := range candidates {
		if s, ok := c.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func intValue(a any, b any, fallback int) int {
	for _, c := range []any{a, b} {
		switch v := c.(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return fallback
}

func firstString(m map[string]any) any {
	for _, v := range m {
		if _, ok := v.(string); ok {
			return v
		}
	}
	return nil
}

func firstNumber(m map[string]any) any {
	for _, v := range m {
		if _, ok := v.(float64); ok {
			return v
		}
	}
	return nil
}
