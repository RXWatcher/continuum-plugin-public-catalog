package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net"
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
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/RXWatcher/silo-plugin-public-catalog/internal/httproutes"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/migrate"
	pluginrt "github.com/RXWatcher/silo-plugin-public-catalog/internal/runtime"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/server"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "silo-plugin-public-catalog"})
	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()
	var standaloneOnce sync.Once
	var standaloneAddr atomic.Value
	var standaloneSrv atomic.Pointer[http.Server]
	var poolPtr atomic.Pointer[pgxpool.Pool]

	var applyConfig func(pluginrt.Config) error
	applyConfig = func(cfg pluginrt.Config) error {
		ctx := context.Background()
		databaseURL := cfg.DatabaseURL

		// Run migrations first so the schema exists before we open the
		// pool with our typed code paths.
		if err := migrate.Run(ctx, databaseURL); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}

		pcfg, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			return fmt.Errorf("parse database_url: %w", err)
		}
		if pcfg.MaxConns < 4 {
			pcfg.MaxConns = 4
		}
		pool, err := pgxpool.NewWithConfig(ctx, pcfg)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		st := store.New(pool)

		// Bootstrap merges manifest values, generates the token secret if
		// missing, and upgrades any legacy plaintext password into a
		// bcrypt hash. After this returns, cfg is the canonical config
		// that survives reinstalls.
		cfg, err = st.Bootstrap(ctx, cfg)
		if err != nil {
			pool.Close()
			return fmt.Errorf("bootstrap config: %w", err)
		}
		cfg.DatabaseURL = databaseURL

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
			DatabaseURL:         cfg.DatabaseURL,
			Logger:              logger,
			TokenSecret:         cfg.TokenSecret,
			PublicBaseURL:       cfg.PublicBaseURL,
			StandaloneListen:    cfg.StandaloneHTTPListen,
			DefaultTokenTTLHour: cfg.TokenTTLHours,
			Sources:             sources,
			ConfigStore:         st,
			ApplyConfig:         applyConfig,
		}))

		if addr := cfg.StandaloneHTTPListen; addr != "" {
			var bindErr error
			started := false
			standaloneOnce.Do(func() {
				started = true
				// Synchronous bind: a port-already-in-use / permission
				// failure has to surface from this Configure RPC, not
				// asynchronously in a logger.Error a few ms later
				// while the plugin reports success. Hand the bound
				// listener off to a goroutine for the serve loop.
				ln, err := net.Listen("tcp", addr)
				if err != nil {
					bindErr = fmt.Errorf("bind standalone listener %q: %w", addr, err)
					return
				}
				if pluginrt.IsWildcardListen(addr) {
					logger.Warn(
						"standalone listener bound to ALL interfaces; prefer 127.0.0.1:port behind a reverse proxy unless this is intentional",
						"addr", addr,
					)
				}
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
				logger.Info("standalone http listener bound", "addr", ln.Addr().String())
				go func() {
					if err := sl.Serve(ln); err != nil && err != http.ErrServerClosed {
						logger.Error("standalone http listener failed", "addr", addr, "err", err)
					}
				}()
			})
			if bindErr != nil {
				return bindErr
			}
			if !started {
				if prev, _ := standaloneAddr.Load().(string); prev != addr {
					// Match `hUpdateConfig`'s admin-API behavior: a
					// listener change once we're bound is an error,
					// not a warning. Operator must revert the addr
					// (configure succeeds again on the old value) or
					// restart the plugin to bind the new one.
					return fmt.Errorf(
						"standalone_http_listen change from %q to %q requires a plugin restart",
						prev, addr,
					)
				}
			}
		}
		if old := poolPtr.Swap(pool); old != nil {
			old.Close()
		}
		logger.Info("configured public-catalog plugin")
		return nil
	}
	rt := pluginrt.New(manifest, applyConfig)

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
