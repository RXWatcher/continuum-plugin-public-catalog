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

func configureRequest(key string, value map[string]any) *pluginv1.ConfigureRequest {
	v, err := structpb.NewStruct(value)
	if err != nil {
		panic(err)
	}
	return &pluginv1.ConfigureRequest{
		Config: []*pluginv1.ConfigEntry{{Key: key, Value: v}},
	}
}
