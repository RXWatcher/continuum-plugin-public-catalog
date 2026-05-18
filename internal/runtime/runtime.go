package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

type Config struct {
	TokenSecret          string
	TokenSecretGenerated bool
	StandaloneHTTPListen string
	PublicPort           int
	PublicBaseURL        string
	AdHTML               string
	CatalogPassword      string
	TokenTTLHours        int
	EbookInstallationID  string
	AudioInstallationID  string
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

func (s *Server) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	cfg := Config{TokenTTLHours: 168}
	for _, e := range req.GetConfig() {
		if e.GetValue() == nil {
			continue
		}
		m := e.GetValue().AsMap()
		switch e.GetKey() {
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
	if cfg.TokenSecret == "" {
		secret, err := autoTokenSecret()
		if err != nil {
			return nil, err
		}
		cfg.TokenSecret = secret
		cfg.TokenSecretGenerated = true
	}
	if len(cfg.TokenSecret) < 32 {
		return nil, fmt.Errorf("token_secret must be at least 32 characters")
	}
	if cfg.PublicBaseURL != "" {
		u, err := url.Parse(cfg.PublicBaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("public_base_url must be an absolute URL")
		}
	}
	if cfg.PublicPort < 0 || cfg.PublicPort > 65535 {
		return nil, fmt.Errorf("public_port must be between 1 and 65535")
	}
	if cfg.PublicPort > 0 && cfg.StandaloneHTTPListen == "" {
		cfg.StandaloneHTTPListen = fmt.Sprintf(":%d", cfg.PublicPort)
	}
	if cfg.TokenTTLHours < 1 {
		cfg.TokenTTLHours = 168
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

func autoTokenSecret() (string, error) {
	path, err := tokenSecretPath()
	if err != nil {
		return "", err
	}
	if data, err := os.ReadFile(path); err == nil {
		secret := string(data)
		if len(secret) >= 32 {
			return secret, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read generated token_secret: %w", err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token_secret: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		return "", fmt.Errorf("persist generated token_secret: %w", err)
	}
	return secret, nil
}

func tokenSecretPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return filepath.Join(filepath.Dir(executable), ".public-catalog-token-secret"), nil
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
