package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	pluginrt "github.com/RXWatcher/silo-plugin-public-catalog/internal/runtime"
	"github.com/RXWatcher/silo-plugin-public-catalog/internal/store"
)

type createTokenRequest struct {
	Hours      int      `json:"hours"`
	LibraryIDs []string `json:"libraryIds"`
	MediaTypes []string `json:"mediaTypes"`
	SaveName   string   `json:"saveName"`
	HTML       string   `json:"html"`
}

func hCreateToken(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTokenRequest
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				if errors.Is(err, io.EOF) {
					req = createTokenRequest{}
				} else {
					writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
					return
				}
			}
		}
		token, err := signToken(d.TokenSecret, tokenClaims{
			Scope:      "catalog",
			LibraryIDs: cleanList(req.LibraryIDs),
			MediaTypes: cleanList(req.MediaTypes),
		})
		if err != nil {
			writeInternal(w, r, d, "token_failed", err)
			return
		}
		path := "catalog?token=" + url.QueryEscape(token)
		link := path
		if base := publicBaseURLForRequest(r, d); base != "" {
			link = strings.TrimRight(base, "/") + "/" + path
		}
		resp := map[string]any{
			"token": token,
			"url":   link,
		}
		if name := strings.TrimSpace(req.SaveName); name != "" {
			if d.ConfigStore == nil {
				writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
				return
			}
			if !validRedirectURL(link) {
				writeErr(w, http.StatusBadRequest, "invalid_url", "generated link URL is not a valid http(s)/relative URL")
				return
			}
			saved, err := d.ConfigStore.SaveCatalogLink(r.Context(), store.SavedCatalogLink{
				Name:  name,
				Token: token,
				URL:   link,
				// Sanitise operator HTML server-side (defence in depth
				// with the admin UI's DOMPurify).
				HTML:       sanitizeHTML(req.HTML),
				MediaTypes: cleanList(req.MediaTypes),
				LibraryIDs: cleanList(req.LibraryIDs),
			})
			if err != nil {
				writeErr(w, http.StatusBadRequest, "save_link_failed", err.Error())
				return
			}
			resp["savedLink"] = saved
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func hDeleteCatalogLink(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		id, err := strconv.Atoi(chi.URLParam(r, "id"))
		if err != nil || id <= 0 {
			writeErr(w, http.StatusBadRequest, "bad_id", "catalog link id is invalid")
			return
		}
		if err := d.ConfigStore.DeleteCatalogLink(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "not_found", "catalog link was not found")
				return
			}
			writeInternal(w, r, d, "delete_link_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func hListCatalogLinks(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeJSON(w, http.StatusOK, []store.SavedCatalogLink{})
			return
		}
		links, err := d.ConfigStore.ListCatalogLinks(r.Context())
		if err != nil {
			writeInternal(w, r, d, "list_links_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, links)
	}
}

func hGetHTMLSection(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"html": publishedHTML(r.Context(), d)})
	}
}

// hSaveHTMLSection persists the operator-edited landing-page HTML into
// the singleton config row (replacing the old on-disk file). 200 KB cap
// matches the prior limit; anything larger is almost certainly a paste
// accident.
func hSaveHTMLSection(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		var req struct {
			HTML string `json:"html"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}
		if len(req.HTML) > 200_000 {
			writeErr(w, http.StatusBadRequest, "html_too_large", "HTML section must be 200 KB or smaller")
			return
		}
		cfg, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeInternal(w, r, d, "config_failed", err)
			return
		}
		// Server-side sanitisation (defence in depth alongside the admin
		// UI's DOMPurify) so stored HTML can never carry script/handlers
		// even if it arrives via a direct API call.
		cfg.PublishedHTML = sanitizeHTML(req.HTML)
		if err := d.ConfigStore.UpdateConfig(r.Context(), cfg); err != nil {
			writeInternal(w, r, d, "html_save_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// hGetConfig returns the current plugin config with hashes/secrets
// redacted. The admin UI uses this to render the settings form.
func hGetConfig(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		cfg, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeInternal(w, r, d, "config_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, redactConfig(cfg))
	}
}

// hUpdateConfig accepts a PATCH of the plugin config. Plaintext
// CatalogPassword is hashed by the store on write. Listener changes
// require a plugin restart and are rejected with a clear error.
func hUpdateConfig(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		cur, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeInternal(w, r, d, "config_failed", err)
			return
		}
		var req pluginrt.Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}
		if req.TokenSecret == "" {
			req.TokenSecret = cur.TokenSecret
		}
		// Empty CatalogPassword + empty hash means "no change". If the
		// admin wants to clear the password they use the dedicated
		// DELETE endpoint or set CatalogPasswordRequired=false.
		if req.CatalogPassword == "" {
			req.CatalogPasswordHash = cur.CatalogPasswordHash
		}
		next, err := pluginrt.NormalizeAppConfig(req)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "config_failed", err.Error())
			return
		}
		if liveListenerChange(cur, next) {
			writeErr(w, http.StatusBadRequest, "config_failed", "changing the public listener requires a plugin restart; keep the current listener settings or restart after updating them")
			return
		}
		if err := d.ConfigStore.UpdateConfig(r.Context(), next); err != nil {
			writeErr(w, http.StatusBadRequest, "config_failed", err.Error())
			return
		}
		fresh, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeInternal(w, r, d, "config_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, redactConfig(fresh))
	}
}

// hClearCatalogPassword zeros the stored hash and forces
// CatalogPasswordRequired off. Operator UX action: "remove the catalog
// password entirely". After this call the catalog is open to anyone.
func hClearCatalogPassword(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigStore == nil {
			writeErr(w, http.StatusServiceUnavailable, "config_store_unavailable", "config storage is not configured")
			return
		}
		if err := d.ConfigStore.ClearCatalogPassword(r.Context()); err != nil {
			writeInternal(w, r, d, "clear_password_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// redactConfig blanks out secret fields before returning the config to
// the admin UI. The UI learns whether a password is set via the boolean
// CatalogPasswordSet flag — the bcrypt hash itself never leaves the
// server.
func redactConfig(cfg pluginrt.Config) pluginrt.Config {
	cfg.TokenSecret = ""
	cfg.CatalogPassword = ""
	cfg.CatalogPasswordSet = strings.TrimSpace(cfg.CatalogPasswordHash) != ""
	cfg.CatalogPasswordHash = ""
	return cfg
}

func liveListenerChange(cur, next pluginrt.Config) bool {
	return strings.TrimSpace(cur.StandaloneHTTPListen) != strings.TrimSpace(next.StandaloneHTTPListen) ||
		cur.PublicPort != next.PublicPort
}
