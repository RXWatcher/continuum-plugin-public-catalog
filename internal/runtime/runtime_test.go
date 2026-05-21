package runtime

import (
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// Token secret generation is no longer the runtime layer's responsibility —
// it's persisted to the DB by store.Bootstrap on first run. The runtime
// only validates that an explicit secret meets the length floor.

func TestConfigureAllowsMissingTokenSecret(t *testing.T) {
	s := New(nil, func(cfg Config) error {
		if cfg.TokenSecret != "" {
			t.Fatalf("TokenSecret should pass through empty when not supplied, got %q", cfg.TokenSecret)
		}
		return nil
	})
	if _, err := s.Configure(t.Context(), configureRequest("database_url", map[string]any{"value": "postgres://x"})); err != nil {
		t.Fatalf("Configure: %v", err)
	}
}

func TestConfigureRejectsShortExplicitTokenSecret(t *testing.T) {
	s := New(nil, nil)
	req := configureRequest("token_secret", map[string]any{"value": "short"})
	if _, err := s.Configure(t.Context(), req); err == nil {
		t.Fatal("expected short explicit token_secret to fail")
	}
}

func TestConfigureMapsPublicPortToStandaloneListenAddress(t *testing.T) {
	s := New(nil, func(cfg Config) error {
		if cfg.PublicPort != 8090 {
			t.Fatalf("PublicPort = %d, want 8090", cfg.PublicPort)
		}
		if cfg.StandaloneHTTPListen != ":8090" {
			t.Fatalf("StandaloneHTTPListen = %q, want :8090", cfg.StandaloneHTTPListen)
		}
		return nil
	})
	req := configureRequest("public_port", map[string]any{"value": float64(8090)})
	if _, err := s.Configure(t.Context(), req); err != nil {
		t.Fatalf("Configure: %v", err)
	}
}

func TestConfigureRejectsInvalidPublicPort(t *testing.T) {
	s := New(nil, nil)
	req := configureRequest("public_port", map[string]any{"value": float64(70000)})
	if _, err := s.Configure(t.Context(), req); err == nil {
		t.Fatal("expected invalid public_port to fail")
	}
}

func TestConfigureAcceptsCatalogPassword(t *testing.T) {
	s := New(nil, func(cfg Config) error {
		if cfg.CatalogPassword != "let-me-in" {
			t.Fatalf("CatalogPassword = %q, want let-me-in", cfg.CatalogPassword)
		}
		return nil
	})
	req := configureRequest("catalog_password", map[string]any{"value": "let-me-in"})
	if _, err := s.Configure(t.Context(), req); err != nil {
		t.Fatalf("Configure: %v", err)
	}
}

func TestValidateListenAddrAcceptsTypicalForms(t *testing.T) {
	for _, addr := range []string{":8090", "127.0.0.1:8090", "[::1]:8090", "host.example:8090"} {
		if err := validateListenAddr(addr); err != nil {
			t.Errorf("validateListenAddr(%q) = %v, want nil", addr, err)
		}
	}
}

func TestValidateListenAddrRejectsMalformedAndOutOfRange(t *testing.T) {
	cases := []string{
		"",
		"8090",      // bare port, no host:port shape
		":99999",    // out-of-range port
		":abc",      // non-numeric port
		"127.0.0.1", // missing port
	}
	for _, addr := range cases {
		if err := validateListenAddr(addr); err == nil {
			t.Errorf("validateListenAddr(%q) accepted; want error", addr)
		}
	}
}

func TestNormalizeAppConfigRejectsBadListenAddr(t *testing.T) {
	_, err := NormalizeAppConfig(Config{StandaloneHTTPListen: ":99999"})
	if err == nil {
		t.Fatal("NormalizeAppConfig accepted :99999; want validation error")
	}
}

func TestIsWildcardListen(t *testing.T) {
	wildcards := []string{":8090", "0.0.0.0:8090", "[::]:8090"}
	for _, a := range wildcards {
		if !IsWildcardListen(a) {
			t.Errorf("IsWildcardListen(%q) = false, want true", a)
		}
	}
	specific := []string{"", "127.0.0.1:8090", "[::1]:8090", "host.example:8090"}
	for _, a := range specific {
		if IsWildcardListen(a) {
			t.Errorf("IsWildcardListen(%q) = true, want false", a)
		}
	}
}

func configureRequest(key string, value map[string]any) *pluginv1.ConfigureRequest {
	v, err := structpb.NewStruct(value)
	if err != nil {
		panic(err)
	}
	db, err := structpb.NewStruct(map[string]any{"value": "postgres://x"})
	if err != nil {
		panic(err)
	}
	return &pluginv1.ConfigureRequest{
		Config: []*pluginv1.ConfigEntry{
			{Key: "database_url", Value: db},
			{Key: key, Value: v},
		},
	}
}
