package server

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hashicorp/go-hclog"

	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

type Host interface {
	ListLibraryMedia(ctx context.Context, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error)
	GetCatalogStats(ctx context.Context, libraryIDs []string) (*runtimehost.CatalogStats, error)
	CallPluginHTTP(ctx context.Context, req runtimehost.CallPluginHTTPRequest) (*runtimehost.CallPluginHTTPResponse, error)
	CallPluginJSON(ctx context.Context, req runtimehost.CallPluginJSONRequest) error
}

type Deps struct {
	Host                func() Host
	Logger              hclog.Logger
	TokenSecret         string
	PublicBaseURL       string
	AdHTML              string
	DefaultTokenTTLHour int
	StatsCacheTTL       time.Duration
	Sources             []CatalogSource

	statsCache *statsCache
}

func New(d Deps) http.Handler {
	if d.StatsCacheTTL <= 0 {
		d.StatsCacheTTL = 30 * time.Second
	}
	d.statsCache = &statsCache{ttl: d.StatsCacheTTL}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	r.Get("/", hLanding(d))
	r.Get("/catalog", hCatalogPage(d))
	r.Get("/api/public/stats", hStats(d))
	r.Get("/api/catalog/media", hCatalogMedia(d))
	r.Post("/api/admin/catalog-token", requireAdmin(hCreateToken(d)))
	r.Get("/admin", requireAdmin(hAdminPage(d)))
	return r
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "base-uri 'none'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/catalog") || strings.HasPrefix(r.URL.Path, "/api/catalog/") {
			h.Set("Cache-Control", "no-store")
		} else if strings.HasPrefix(r.URL.Path, "/api/public/") {
			h.Set("Cache-Control", "public, max-age=30")
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("X-Continuum-User-Role"), "admin") {
			next(w, r)
			return
		}
		roles := strings.Split(r.Header.Get("X-Continuum-User-Roles"), ",")
		for _, role := range roles {
			if strings.EqualFold(strings.TrimSpace(role), "admin") {
				next(w, r)
				return
			}
		}
		writeErr(w, http.StatusUnauthorized, "admin_required", "admin identity required")
	}
}

func hLanding(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, _ := hostStats(r.Context(), d, nil)
		total := 0
		if stats != nil {
			total = stats.TotalItems
		}
		writeHTML(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Media Catalog</title>
<style>`+css()+`</style>
</head>
<body>
<main class="shell">
  <section class="hero">
    <div>
      <p class="eyebrow">Public catalog</p>
      <h1>See what is available</h1>
      <p class="lead">Browse current library statistics and featured access information.</p>
    </div>
    <div class="stat"><span>`+strconv.Itoa(total)+`</span><small>items available</small></div>
  </section>
  <section class="panel" id="stats"><h2>Stats</h2>`+statsHTML(stats)+`</section>
  <section class="panel ad"><h2>Featured</h2>`+adHTML(d.AdHTML)+`</section>
</main>
</body>
</html>`)
	}
}

func hCatalogPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if _, err := verifyToken(d.TokenSecret, token, time.Now()); err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid_token", "catalog access token is invalid or expired")
			return
		}
		writeHTML(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Catalog</title>
<style>`+css()+`</style>
</head>
<body>
<main class="shell">
  <section class="toolbar">
    <input id="q" type="search" placeholder="Search catalog">
    <select id="type"><option value="">All media</option><option value="movie">Movies</option><option value="tv">TV</option><option value="episode">Episodes</option><option value="audiobook">Audiobooks</option><option value="ebook">Ebooks</option></select>
    <select id="sort"><option value="title">Title</option><option value="year">Year</option><option value="added_at">Recently added</option><option value="rating">Rating</option></select>
  </section>
  <section id="results" class="grid"></section>
  <button id="more" class="button" hidden>Load more</button>
</main>
<script>
const token = new URLSearchParams(location.search).get('token');
let next = "";
async function load(reset=false) {
  if (reset) { next = ""; document.getElementById('results').innerHTML = ""; }
  const p = new URLSearchParams({token, q: q.value, sort: sort.value, page_size: "48"});
  if (type.value) p.set("media_type", type.value);
  if (next) p.set("page_token", next);
  const res = await fetch("/api/catalog/media?" + p.toString());
  if (!res.ok) { document.getElementById('results').textContent = "Catalog unavailable"; return; }
  const data = await res.json();
  next = data.nextPageToken || "";
  more.hidden = !next;
  for (const it of data.items || []) {
    const card = document.createElement("article");
    card.className = "card";
    card.innerHTML = '<div class="poster">' + (it.posterUrl ? '<img src="'+esc(it.posterUrl)+'" alt="">' : '') + '</div><div class="body"><h3>'+esc(it.title)+'</h3><p>'+esc([it.mediaType, it.year].filter(Boolean).join(" • "))+'</p><p>'+esc((it.genres||[]).slice(0,3).join(", "))+'</p></div>';
    results.appendChild(card);
  }
}
function esc(s) { return String(s ?? "").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
q.addEventListener("input", () => { clearTimeout(window.t); window.t=setTimeout(()=>load(true), 250); });
type.addEventListener("change", () => load(true));
sort.addEventListener("change", () => load(true));
more.addEventListener("click", () => load(false));
load(true);
</script>
</body>
</html>`)
	}
}

func hStats(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := hostStats(r.Context(), d, nil)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "host_unavailable", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

func hCatalogMedia(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := verifyToken(d.TokenSecret, r.URL.Query().Get("token"), time.Now())
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid_token", "catalog access token is invalid or expired")
			return
		}
		host := d.Host()
		if host == nil {
			writeErr(w, http.StatusServiceUnavailable, "host_unavailable", "continuum host is not connected")
			return
		}
		q := r.URL.Query()
		mediaTypes, ok := mergeMediaTypes(claims.MediaTypes, q["media_type"])
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_media_type", "requested media type is not available for this catalog link")
			return
		}
		req := runtimehost.ListLibraryMediaRequest{
			LibraryIDs: claims.LibraryIDs,
			MediaTypes: mediaTypes,
			Query:      strings.TrimSpace(q.Get("q")),
			Genre:      strings.TrimSpace(q.Get("genre")),
			Sort:       defaultString(q.Get("sort"), "title"),
			Descending: q.Get("desc") == "true",
			PageSize:   boundedInt(q.Get("page_size"), 48, 1, 100),
			PageToken:  q.Get("page_token"),
		}
		req.YearMin = boundedInt(q.Get("year_min"), 0, 0, 9999)
		req.YearMax = boundedInt(q.Get("year_max"), 0, 0, 9999)
		if len(req.MediaTypes) == 1 {
			if src := sourceForType(d.Sources, req.MediaTypes[0]); src != nil {
				resp, err := src.List(r.Context(), host, req)
				if err != nil {
					writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
					return
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}
		resp, err := host.ListLibraryMedia(r.Context(), req)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "catalog_failed", err.Error())
			return
		}
		if req.PageToken == "" {
			resp = appendSourceCatalogs(r.Context(), host, resp, d.Sources, req)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

type createTokenRequest struct {
	Hours      int      `json:"hours"`
	LibraryIDs []string `json:"libraryIds"`
	MediaTypes []string `json:"mediaTypes"`
}

func hCreateToken(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTokenRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		hours := req.Hours
		if hours < 1 {
			hours = d.DefaultTokenTTLHour
		}
		exp := time.Now().Add(time.Duration(hours) * time.Hour)
		token, err := signToken(d.TokenSecret, tokenClaims{
			Scope:      "catalog",
			ExpiresAt:  exp.Unix(),
			LibraryIDs: cleanList(req.LibraryIDs),
			MediaTypes: cleanList(req.MediaTypes),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "token_failed", err.Error())
			return
		}
		path := "/catalog?token=" + url.QueryEscape(token)
		link := path
		if d.PublicBaseURL != "" {
			link = strings.TrimRight(d.PublicBaseURL, "/") + path
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"token":     token,
			"url":       link,
			"expiresAt": exp.UTC().Format(time.RFC3339),
		})
	}
}

func hAdminPage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Public Catalog Admin</title><style>`+css()+`</style></head><body><main class="shell"><section class="panel"><h1>Public Catalog</h1><button class="button" id="gen">Generate catalog link</button><pre id="out"></pre></section></main><script>gen.onclick=async()=>{const r=await fetch("/api/admin/catalog-token",{method:"POST",headers:{"Content-Type":"application/json"},body:"{}"}); out.textContent=JSON.stringify(await r.json(),null,2)}</script></body></html>`)
	}
}

func hostStats(ctx context.Context, d Deps, libraryIDs []string) (*runtimehost.CatalogStats, error) {
	if d.statsCache != nil {
		return d.statsCache.get(ctx, d.Host, libraryIDs, d.Sources)
	}
	host := d.Host()
	if host == nil {
		return nil, http.ErrServerClosed
	}
	stats, err := host.GetCatalogStats(ctx, libraryIDs)
	if err != nil {
		return nil, err
	}
	return withSourceStats(ctx, host, stats, d.Sources)
}

type statsCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]statsCacheEntry
}

type statsCacheEntry struct {
	expires time.Time
	stats   *runtimehost.CatalogStats
}

func (c *statsCache) get(ctx context.Context, hostFn func() Host, libraryIDs []string, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	key := statsCacheKey(libraryIDs)
	now := time.Now()
	c.mu.Lock()
	if c.entries != nil {
		if ent, ok := c.entries[key]; ok && now.Before(ent.expires) {
			stats := ent.stats
			c.mu.Unlock()
			return stats, nil
		}
	}
	c.mu.Unlock()

	host := hostFn()
	if host == nil {
		return nil, http.ErrServerClosed
	}
	stats, err := host.GetCatalogStats(ctx, libraryIDs)
	if err != nil {
		return nil, err
	}
	stats, err = withSourceStats(ctx, host, stats, sources)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]statsCacheEntry{}
	}
	c.entries[key] = statsCacheEntry{expires: now.Add(c.ttl), stats: stats}
	c.mu.Unlock()
	return stats, nil
}

func withSourceStats(ctx context.Context, host Host, base *runtimehost.CatalogStats, sources []CatalogSource) (*runtimehost.CatalogStats, error) {
	if base == nil {
		base = &runtimehost.CatalogStats{}
	}
	for _, src := range sources {
		if src == nil {
			continue
		}
		stats, err := src.Stats(ctx, host)
		if err != nil {
			return nil, err
		}
		base.TotalItems += stats.TotalItems
		base.MediaTypeCounts = append(base.MediaTypeCounts, stats.MediaTypeCounts...)
		base.LibraryCounts = append(base.LibraryCounts, stats.LibraryCounts...)
	}
	return base, nil
}

func appendSourceCatalogs(ctx context.Context, host Host, base *runtimehost.ListLibraryMediaResponse, sources []CatalogSource, req runtimehost.ListLibraryMediaRequest) *runtimehost.ListLibraryMediaResponse {
	if base == nil {
		base = &runtimehost.ListLibraryMediaResponse{}
	}
	for _, src := range sources {
		if src == nil || !mediaTypeRequested(req.MediaTypes, src.MediaType()) {
			continue
		}
		sourceReq := req
		sourceReq.MediaTypes = []string{src.MediaType()}
		if sourceReq.PageSize <= 0 || sourceReq.PageSize > 12 {
			sourceReq.PageSize = 12
		}
		resp, err := src.List(ctx, host, sourceReq)
		if err != nil || resp == nil {
			continue
		}
		base.Items = append(base.Items, resp.Items...)
		base.TotalCount += resp.TotalCount
	}
	return base
}

func mediaTypeRequested(requested []string, mediaType string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, v := range requested {
		if v == mediaType {
			return true
		}
	}
	return false
}

func statsCacheKey(libraryIDs []string) string {
	ids := cleanList(libraryIDs)
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
}

func statsHTML(stats *runtimehost.CatalogStats) string {
	if stats == nil {
		return `<p>Stats are not available yet.</p>`
	}
	var b strings.Builder
	b.WriteString(`<div class="stats">`)
	for _, c := range stats.MediaTypeCounts {
		b.WriteString(`<div><strong>`)
		b.WriteString(strconv.Itoa(c.Count))
		b.WriteString(`</strong><span>`)
		b.WriteString(html.EscapeString(c.MediaType))
		b.WriteString(`</span></div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func adHTML(v string) string {
	if strings.TrimSpace(v) == "" {
		return `<p>Contact us for access and availability.</p>`
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func boundedInt(raw string, fallback, min, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
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

func css() string {
	return `body{margin:0;font-family:Inter,system-ui,-apple-system,Segoe UI,sans-serif;background:#0f1115;color:#f5f7fb} .shell{max-width:1180px;margin:0 auto;padding:32px 20px}.hero{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;padding:48px 0}.eyebrow{color:#8eb6ff;text-transform:uppercase;font-size:12px;letter-spacing:.08em}.hero h1{font-size:44px;line-height:1.05;margin:0 0 12px}.lead{color:#b8c0cf;font-size:18px}.stat{border:1px solid #2b3342;border-radius:8px;padding:20px;min-width:180px;background:#171b23}.stat span{display:block;font-size:36px;font-weight:700}.stat small{color:#aab3c2}.panel{border-top:1px solid #2b3342;padding:28px 0}.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px}.stats div,.card{background:#171b23;border:1px solid #2b3342;border-radius:8px}.stats div{padding:16px}.stats strong{display:block;font-size:26px}.stats span,.card p{color:#aab3c2}.toolbar{display:grid;grid-template-columns:1fr 160px 160px;gap:10px;position:sticky;top:0;background:#0f1115;padding:16px 0}input,select,.button{border:1px solid #344052;background:#171b23;color:#f5f7fb;border-radius:6px;padding:10px 12px}.button{cursor:pointer}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:14px}.poster{aspect-ratio:2/3;background:#222936;border-radius:8px 8px 0 0;overflow:hidden}.poster img{width:100%;height:100%;object-fit:cover}.body{padding:12px}.body h3{font-size:15px;margin:0 0 8px}@media(max-width:700px){.hero{display:block}.toolbar{grid-template-columns:1fr}.hero h1{font-size:34px}}`
}
