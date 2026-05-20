package server

import (
	"bytes"
	"embed"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"strings"

	"github.com/ContinuumApp/continuum-plugin-public-catalog/internal/store"
)

//go:embed public/dist/* public/dist/assets/*
var publicSPA embed.FS

// publicBootstrap is the per-request JSON payload baked into the SPA
// index.html. The SPA reads it from window.__PUBLIC_CATALOG_BOOTSTRAP
// and uses it to decide what route to render, what theme to use, and
// whether to prompt for the catalog password.
type publicBootstrap struct {
	Mode                 string                   `json:"mode"`
	Theme                string                   `json:"theme"`
	CatalogHref          string                   `json:"catalogHref"`
	CustomHTML           string                   `json:"customHTML"`
	AuthRequired         bool                     `json:"authRequired"`
	Token                string                   `json:"token"`
	InitialStats         *store.CatalogStats      `json:"initialStats,omitempty"`
	InitialLibraryID     string                   `json:"initialLibraryId,omitempty"`
	InitialItems         []store.CatalogMediaItem `json:"initialItems,omitempty"`
	InitialNextPageToken string                   `json:"initialNextPageToken,omitempty"`
	InitialTotalCount    int                      `json:"initialTotalCount,omitempty"`
	InitialFilters       *store.CatalogFilters    `json:"initialFilters,omitempty"`
}

func hPublicAsset() http.HandlerFunc {
	dist, err := fs.Sub(publicSPA, "public/dist")
	if err != nil {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		}
	}
	handler := http.StripPrefix("/", http.FileServer(http.FS(dist)))
	return func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}
}

func writePublicApp(w http.ResponseWriter, r *http.Request, bootstrap publicBootstrap, status int) {
	if bootstrap.Theme == "" || bootstrap.Theme == "default" {
		bootstrap.Theme = "midnight-cinema"
	}
	if bootstrap.CatalogHref == "" {
		bootstrap.CatalogHref = "catalog"
	}
	index, err := publicSPA.ReadFile("public/dist/index.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "spa_unavailable", "public catalog app has not been built")
		return
	}
	rawBootstrap, err := json.Marshal(bootstrap)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bootstrap_failed", "failed to render bootstrap")
		return
	}
	index = bytes.Replace(index, []byte("%PUBLIC_CATALOG_BOOTSTRAP%"), rawBootstrap, 1)
	index = bytes.Replace(index, []byte(`<html lang="en">`), []byte(`<html lang="en" data-theme="`+html.EscapeString(bootstrap.Theme)+`">`), 1)
	assetBase := "./assets/"
	if strings.HasPrefix(r.URL.Path, "/item/") {
		assetBase = "../assets/"
	}
	index = bytes.ReplaceAll(index, []byte(`./assets/`), []byte(assetBase))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(index)
}

// adminTheme picks the theme to render the admin SPA with. Falls back
// to the host-provided X-Continuum-Theme header so the admin matches
// the rest of Continuum's UI by default.
func adminTheme(r *http.Request) string {
	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = r.Header.Get("X-Continuum-Theme")
	}
	if theme == "" {
		theme = r.Header.Get("X-Continuum-User-Theme")
	}
	if theme == "" {
		theme = "default"
	}
	return html.EscapeString(theme)
}
