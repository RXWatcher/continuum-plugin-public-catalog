package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// maxCatalogOffset is the absolute ceiling on how deep a client may
// paginate into a catalog result set. Even a validly-signed page token
// is rejected past this point. It exists as a hard floor against
// enumeration / deep-scan: legitimate browsing never walks tens of
// thousands of items deep, but an attacker scripting ?page_token= would.
// At the max page size of 100 this still allows ~500 pages.
const maxCatalogOffset = 50000

var (
	errPageTokenInvalid = errors.New("invalid page token")
	errPageTokenTooDeep = errors.New("page token exceeds maximum offset")
)

// signPageToken wraps an opaque inner page-token value (as produced by
// the store or host SDK — e.g. "offset:48") in an HMAC envelope keyed by
// the server's TokenSecret. The result is "<payload>.<sig>" where
// payload is the base64url-encoded inner value. This stops a client from
// hand-crafting a page_token to enumerate past the result set: only the
// server can mint a valid token, and it only ever mints them for the
// next legitimate page.
//
// An empty inner value yields an empty token (no further pages).
func signPageToken(secret, inner string) string {
	if inner == "" {
		return ""
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(inner))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// verifyPageToken validates a signed page token and returns the opaque
// inner value. An empty token is valid and means "first page" (inner
// "").  A non-empty token must carry a valid HMAC signature, decode
// cleanly, and — when it encodes an "offset:N" value — sit within the
// absolute offset cap.
func verifyPageToken(secret, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", nil
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errPageTokenInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want) {
		return "", errPageTokenInvalid
	}
	rawInner, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errPageTokenInvalid
	}
	inner := string(rawInner)
	if off, ok := parseOffsetToken(inner); ok && off > maxCatalogOffset {
		return "", errPageTokenTooDeep
	}
	return inner, nil
}

// parseOffsetToken extracts N from an "offset:N" inner token. Returns
// ok=false for any other shape (e.g. host-SDK cursor strings), which the
// caller treats as "not an offset, no cap to enforce here".
func parseOffsetToken(inner string) (int, bool) {
	const prefix = "offset:"
	if !strings.HasPrefix(inner, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(inner, prefix))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
