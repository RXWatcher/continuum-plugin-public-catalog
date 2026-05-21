package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RXWatcher/continuum-plugin-public-catalog/internal/store"
)

// hCatalogLogin validates a supplied password against the stored bcrypt
// hash (or, transparently, against a legacy plaintext value still in
// the DB), and on success issues a long-lived session cookie scoped to
// catalog browsing. The cookie is signed with TokenSecret; rotating
// that secret invalidates all outstanding sessions.
func hCatalogLogin(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
			return
		}

		cfg, err := d.ConfigStore.GetConfig(r.Context())
		if err != nil {
			writeInternal(w, r, d, "config_failed", err)
			return
		}

		// Catalog is open: no password required regardless of whether a
		// hash is stored. Issue the session cookie anyway so the SPA
		// gets a stable signal back.
		if !cfg.CatalogPasswordRequired {
			issueCatalogSession(w, r, d.TokenSecret)
			return
		}

		// No password ever set: reject login attempts rather than
		// pretend they succeed; the operator should disable
		// CatalogPasswordRequired instead.
		if cfg.CatalogPasswordHash == "" && cfg.CatalogPassword == "" {
			writeErr(w, http.StatusUnauthorized, "invalid_password", "catalog password is invalid")
			return
		}

		switch store.VerifyCatalogPassword(cfg.CatalogPasswordHash, cfg.CatalogPassword, req.Password) {
		case store.CatalogPasswordOK:
			issueCatalogSession(w, r, d.TokenSecret)
		case store.CatalogPasswordOKRehash:
			// Background upgrade so a slow bcrypt write doesn't block
			// the login response.
			go func(pw string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := d.ConfigStore.RehashCatalogPassword(ctx, pw); err != nil && d.Logger != nil {
					d.Logger.Warn("public-catalog: password rehash failed", "err", err)
				}
			}(req.Password)
			issueCatalogSession(w, r, d.TokenSecret)
		default:
			writeErr(w, http.StatusUnauthorized, "invalid_password", "catalog password is invalid")
		}
	}
}

func issueCatalogSession(w http.ResponseWriter, r *http.Request, secret string) {
	token, err := signToken(secret, tokenClaims{Scope: "catalog"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token_failed", "failed to sign session token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "public_catalog_auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// catalogClaimsFromRequest extracts the active catalog session from
// either the ?token= query param or the public_catalog_auth cookie.
// When CatalogPasswordRequired is false (or the DB is unreachable),
// returns a permissive catalog-scope claim so the catalog is open.
func catalogClaimsFromRequest(d Deps, r *http.Request) (tokenClaims, bool) {
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		claims, err := verifyToken(d.TokenSecret, token, time.Now())
		return claims, err == nil
	}
	if cookie, err := r.Cookie("public_catalog_auth"); err == nil && cookie.Value != "" {
		claims, err := verifyToken(d.TokenSecret, cookie.Value, time.Now())
		return claims, err == nil
	}
	if d.ConfigStore != nil {
		if cfg, err := d.ConfigStore.GetConfig(r.Context()); err == nil && !cfg.CatalogPasswordRequired {
			return tokenClaims{Scope: "catalog"}, true
		}
	}
	return tokenClaims{}, false
}
