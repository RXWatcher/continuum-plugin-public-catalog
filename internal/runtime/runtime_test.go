package runtime

import (
	"testing"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConfigureGeneratesTokenSecretWhenMissing(t *testing.T) {
	s := New(nil, func(cfg Config) error {
		if len(cfg.TokenSecret) < 32 {
			t.Fatalf("generated token secret length = %d, want at least 32", len(cfg.TokenSecret))
		}
		return nil
	})

	if _, err := s.Configure(t.Context(), nil); err != nil {
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

func configureRequest(key string, value map[string]any) *pluginv1.ConfigureRequest {
	v, err := structpb.NewStruct(value)
	if err != nil {
		panic(err)
	}
	return &pluginv1.ConfigureRequest{
		Config: []*pluginv1.ConfigEntry{{Key: key, Value: v}},
	}
}
