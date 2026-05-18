package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/httproutes"
	pluginrt "github.com/ContinuumApp/continuum-plugin-public-catalog/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/server"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-public-catalog"})
	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()
	var standaloneOnce sync.Once
	var standaloneAddr atomic.Value
	var standaloneSrv atomic.Pointer[http.Server]

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		sources := []server.CatalogSource{}
		if id, err := strconv.Atoi(cfg.EbookInstallationID); err == nil && id > 0 {
			sources = append(sources, server.NewRuntimeHostSource("ebook", id))
		}
		if id, err := strconv.Atoi(cfg.AudioInstallationID); err == nil && id > 0 {
			sources = append(sources, server.NewRuntimeHostSource("audiobook", id))
		}
		httpSrv.SetHandler(server.New(server.Deps{
			Host: func() server.Host {
				return sdkruntime.Host()
			},
			Logger:               logger,
			TokenSecret:          cfg.TokenSecret,
			TokenSecretGenerated: cfg.TokenSecretGenerated,
			PublicBaseURL:        cfg.PublicBaseURL,
			AdHTML:               cfg.AdHTML,
			DefaultTokenTTLHour:  cfg.TokenTTLHours,
			Sources:              sources,
		}))

		if addr := cfg.StandaloneHTTPListen; addr != "" {
			started := false
			standaloneOnce.Do(func() {
				started = true
				standaloneAddr.Store(addr)
				sl := &http.Server{
					Addr:              addr,
					Handler:           httpSrv,
					ReadHeaderTimeout: 10 * time.Second,
					ReadTimeout:       30 * time.Second,
					WriteTimeout:      60 * time.Second,
					IdleTimeout:       120 * time.Second,
				}
				standaloneSrv.Store(sl)
				go func() {
					logger.Info("standalone http listener starting", "addr", addr)
					if err := sl.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						logger.Error("standalone http listener failed", "addr", addr, "err", err)
					}
				}()
			})
			if !started {
				if prev, _ := standaloneAddr.Load().(string); prev != addr {
					logger.Warn("standalone_http_listen changed; restart the plugin to apply", "current", prev, "requested", addr)
				}
			}
		}
		logger.Info("configured public-catalog plugin")
		return nil
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		if sl := standaloneSrv.Load(); sl != nil {
			logger.Info("draining standalone http listener", "addr", sl.Addr)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := sl.Shutdown(ctx); err != nil {
				logger.Warn("standalone http drain returned error", "err", err)
			}
		}
	}()

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime:    rt,
			HttpRoutes: httpSrv,
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{{Os: goruntime.GOOS, Arch: goruntime.GOARCH}}
	}
	return manifest, nil
}
