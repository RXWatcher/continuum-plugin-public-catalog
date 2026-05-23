package server

import (
	"net/http"
	"strings"
)

// securityHeaders applies a small set of response hardening defaults
// matching the rest of Silo. Catalog pages get Cache-Control:
// no-store so a shared cache (browser or reverse proxy) can't leak
// session-gated catalog HTML, and public API responses pick up
// no-store too since they're session-dependent.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "base-uri 'none'; frame-ancestors 'none'")
		switch {
		case strings.HasPrefix(r.URL.Path, "/catalog"),
			strings.HasPrefix(r.URL.Path, "/item/"),
			strings.HasPrefix(r.URL.Path, "/api/catalog/"),
			strings.HasPrefix(r.URL.Path, "/api/public/"):
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin gates admin routes on the host-stamped identity header.
// The host strips inbound X-Silo-* identity headers from public
// requests before forwarding to the plugin, so seeing X-Silo-User-Role
// here is a trustworthy signal.
func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Silo-User-Id") == "" {
			writeErr(w, http.StatusUnauthorized, "unauthenticated", "admin login required")
			return
		}
		if r.Header.Get("X-Silo-User-Role") != "admin" {
			writeErr(w, http.StatusForbidden, "forbidden", "admin access required")
			return
		}
		next(w, r)
	}
}
