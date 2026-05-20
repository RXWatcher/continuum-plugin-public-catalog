package server

import (
	"strconv"
	"strings"

	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

func boundedInt(raw string, fallback, minV, maxV int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < minV {
		return minV
	}
	if n > maxV {
		return maxV
	}
	return n
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func defaultPositive(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// mergeMediaTypes intersects a bypass-token's allowed media types with
// the request's requested media types. Returns (merged, ok=false) when
// the request asks for something the token doesn't permit, so callers
// can return 400 instead of silently dropping.
func mergeMediaTypes(allowed, requested []string) ([]string, bool) {
	requested, requestedOK := cleanMediaTypes(requested)
	if !requestedOK {
		return nil, false
	}
	allowed, allowedOK := cleanMediaTypes(allowed)
	if !allowedOK {
		return nil, false
	}
	if len(allowed) == 0 {
		return requested, true
	}
	if len(requested) == 0 {
		return allowed, true
	}
	ok := map[string]bool{}
	for _, v := range allowed {
		ok[v] = true
	}
	out := []string{}
	for _, v := range requested {
		if ok[v] {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

func mergeLibraryIDs(allowed, requested []string) ([]string, bool) {
	requested = cleanList(requested)
	allowed = cleanList(allowed)
	if len(allowed) == 0 {
		return requested, true
	}
	if len(requested) == 0 {
		return allowed, true
	}
	ok := map[string]bool{}
	for _, v := range allowed {
		ok[v] = true
	}
	out := []string{}
	for _, v := range requested {
		if ok[v] {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

func cleanMediaTypes(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, v := range cleanList(in) {
		switch strings.ToLower(v) {
		case "movie":
			out = append(out, "movie")
		case "tv", "series":
			out = append(out, "tv")
		case "episode":
			out = append(out, "episode")
		case "audiobook":
			out = append(out, "audiobook")
		case "ebook":
			out = append(out, "ebook")
		default:
			return nil, false
		}
	}
	return out, true
}

// directCatalogMediaTypes returns true when every requested media type
// is one the local DB path can serve. When false, the handler falls
// back to the host SDK / federated sources.
func directCatalogMediaTypes(mediaTypes []string) bool {
	if len(mediaTypes) == 0 {
		return true
	}
	for _, mt := range mediaTypes {
		switch strings.ToLower(mt) {
		case "movie", "tv", "series", "episode":
			continue
		default:
			return false
		}
	}
	return true
}

// normalizeStoreMediaTypes maps the wire types to the values the
// store's SQL filters expect. The wire uses "tv" but the store uses
// "series" (matching public.media_items.type), so we rewrite "tv" to
// "series" here. Empty input passes through as an empty slice so the
// store query can drop the media_type filter entirely.
func normalizeStoreMediaTypes(mediaTypes []string) []string {
	out := []string{}
	for _, mediaType := range mediaTypes {
		if mediaType == "tv" {
			mediaType = "series"
		}
		if mediaType != "" {
			out = append(out, mediaType)
		}
	}
	return out
}

func emptyCatalogMediaResponse() runtimehost.ListLibraryMediaResponse {
	return runtimehost.ListLibraryMediaResponse{}
}
