package server

import (
	"net/url"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
)

// htmlPolicy is the server-side sanitisation policy applied to all
// operator-supplied HTML before it is persisted. This is defence in
// depth: the admin UI also sanitises with DOMPurify, but we never trust
// client-side sanitisation as the only barrier — a crafted API call
// bypasses the SPA entirely. The policy allows the rich-but-safe subset
// the landing/link HTML editors are meant to produce and strips scripts,
// event handlers, and dangerous URL schemes.
var (
	htmlPolicyOnce sync.Once
	htmlPolicy     *bluemonday.Policy
)

func sanitizeHTML(in string) string {
	htmlPolicyOnce.Do(func() {
		p := bluemonday.UGCPolicy()
		// UGCPolicy already permits common formatting, lists, tables,
		// links, and images, and forces rel="nofollow" + safe URL
		// schemes. Allow class attributes so operator HTML can hook the
		// catalog's existing CSS (e.g. the custom-html-sample styling).
		p.AllowAttrs("class").Globally()
		p.AllowAttrs("style").Globally()
		// Restrict styles to a safe, presentational subset; bluemonday
		// drops url()/expression()/etc. that aren't explicitly allowed.
		p.AllowStyles("text-align", "color", "background-color", "font-weight", "font-style").Globally()
		// Permit https/http/mailto/relative links and image sources only.
		p.AllowURLSchemes("https", "http", "mailto")
		p.RequireParseableURLs(true)
		p.AllowRelativeURLs(true)
		htmlPolicy = p
	})
	return htmlPolicy.Sanitize(in)
}

// validRedirectURL reports whether a saved-link URL is safe to hand back
// to a browser as a navigation target: it must be an http(s) absolute URL
// or a relative URL. Anything with a javascript:, data:, or other
// scheme is rejected so a saved link can't smuggle script execution.
func validRedirectURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "" {
		// Relative URL (path / query / fragment). Reject protocol-
		// relative ("//host") forms which a browser treats as absolute.
		return !strings.HasPrefix(raw, "//")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}
