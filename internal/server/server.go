package server

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
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
	Host                 func() Host
	Logger               hclog.Logger
	TokenSecret          string
	TokenSecretGenerated bool
	PublicBaseURL        string
	AdHTML               string
	DefaultTokenTTLHour  int
	StatsCacheTTL        time.Duration
	Sources              []CatalogSource

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
<html lang="en" data-theme="`+adminTheme(r)+`">
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
<html lang="en" data-theme="`+adminTheme(r)+`">
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
  const res = await fetch("api/catalog/media?" + p.toString());
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
			writeJSON(w, http.StatusOK, runtimehost.CatalogStats{})
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
		host := currentHost(d)
		if host == nil {
			writeJSON(w, http.StatusOK, emptyCatalogMediaResponse())
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
					writeErr(w, http.StatusBadGateway, "catalog_source_unavailable", "catalog source is unavailable")
					return
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}
		resp, err := host.ListLibraryMedia(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusOK, emptyCatalogMediaResponse())
			return
		}
		if req.PageToken == "" {
			resp = appendSourceCatalogs(r.Context(), host, resp, d.Sources, req)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func emptyCatalogMediaResponse() runtimehost.ListLibraryMediaResponse {
	return runtimehost.ListLibraryMediaResponse{
		Items:      []runtimehost.CatalogMediaItem{},
		TotalCount: 0,
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
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				if errors.Is(err, io.EOF) {
					req = createTokenRequest{}
				} else {
					writeErr(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
					return
				}
			}
		}
		hours := req.Hours
		if hours < 1 {
			hours = d.DefaultTokenTTLHour
		}
		if hours < 1 {
			hours = 168
		}
		if hours > 24*365 {
			hours = 24 * 365
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
		path := "catalog?token=" + url.QueryEscape(token)
		link := path
		if d.PublicBaseURL != "" {
			link = strings.TrimRight(d.PublicBaseURL, "/") + "/" + path
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
		secretStatus := "Auto-generated"
		if strings.TrimSpace(d.TokenSecret) != "" && !d.TokenSecretGenerated {
			secretStatus = "Configured"
		}
		baseURL := d.PublicBaseURL
		if baseURL == "" {
			baseURL = "Relative plugin URLs"
		}
		writeHTML(w, `<!doctype html>
<html lang="en" data-theme="`+adminTheme(r)+`">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Public Catalog Admin</title>
<style>`+css()+adminCSS()+`</style>
</head>
<body>
<main class="shell admin-shell">
  <section class="hero admin-hero">
    <div>
      <a class="button" href="/admin/plugins">&larr; Back to plugins</a>
      <p class="eyebrow">Plugin administration</p>
      <h1>Public Catalog</h1>
      <p class="lead">Create signed catalog links and check the public catalog status.</p>
    </div>
    <a class="button" href="../" target="_blank" rel="noreferrer">Open public page</a>
  </section>

  <section class="admin-grid">
    <div class="panel admin-panel">
      <div class="panel-head">
        <h2>Generate Link</h2>
        <span id="state" class="status-pill">Ready</span>
      </div>
      <form id="tokenForm" class="admin-form">
        <label>Expires in hours
          <input id="hours" name="hours" type="number" min="1" max="8760" value="168">
        </label>
        <fieldset>
          <legend>Media types</legend>
          <label class="check"><input type="checkbox" name="mediaTypes" value="movie" checked> Movies</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="tv" checked> TV</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="episode" checked> Episodes</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="ebook"> Ebooks</label>
          <label class="check"><input type="checkbox" name="mediaTypes" value="audiobook"> Audiobooks</label>
        </fieldset>
        <label>Library IDs
          <input id="libraryIds" name="libraryIds" placeholder="Optional comma-separated IDs">
        </label>
        <button class="button primary" type="submit">Generate catalog link</button>
      </form>
      <div id="result" class="result-box" hidden>
        <label>Catalog URL
          <textarea id="urlOut" readonly rows="3"></textarea>
        </label>
        <div class="actions">
          <button class="button" id="copy" type="button">Copy</button>
          <a class="button" id="openLink" href="#" target="_blank" rel="noreferrer">Open</a>
        </div>
        <dl class="details compact">
          <dt>Expires</dt><dd id="expiresOut"></dd>
        </dl>
      </div>
      <pre id="errorOut" class="error-box" hidden></pre>
    </div>

    <aside class="panel admin-panel">
      <h2>Status</h2>
      <dl class="details">
        <dt>Token secret</dt><dd>`+html.EscapeString(secretStatus)+`</dd>
        <dt>Default TTL</dt><dd>`+strconv.Itoa(defaultPositive(d.DefaultTokenTTLHour, 168))+` hours</dd>
        <dt>Public base URL</dt><dd>`+html.EscapeString(baseURL)+`</dd>
        <dt>Trusted HTML</dt><dd>Operator-provided landing-page HTML only; never paste user content.</dd>
        <dt>Extra sources</dt><dd>`+strconv.Itoa(len(d.Sources))+` configured</dd>
      </dl>
      <button class="button" id="refreshStats" type="button">Refresh stats</button>
      <div id="statsOut" class="stats slim"><div><strong>...</strong><span>Loading</span></div></div>
    </aside>
  </section>
</main>
<script>
const params=new URLSearchParams(location.search);
const hostToken=params.get("token")||"";
if(params.has("token")){params.delete("token");history.replaceState(null,"",location.pathname+(params.toString()?"?"+params.toString():"")+location.hash)}
function headers(){const h={"Content-Type":"application/json"};if(hostToken)h.Authorization="Bearer "+hostToken;return h}
function list(name){return [...document.querySelectorAll('[name="'+name+'"]:checked')].map(el=>el.value)}
function csv(id){return document.getElementById(id).value.split(",").map(v=>v.trim()).filter(Boolean)}
function setState(text,bad=false){state.textContent=text;state.className=bad?"status-pill bad":"status-pill"}
function showError(message){errorOut.hidden=false;errorOut.textContent=message;setState("Error",true)}
function hideError(){errorOut.hidden=true;errorOut.textContent=""}
tokenForm.addEventListener("submit",async event=>{
  event.preventDefault(); hideError(); setState("Generating");
  const body={hours:Number(hours.value)||0,mediaTypes:list("mediaTypes"),libraryIds:csv("libraryIds")};
  try{
    const r=await fetch("api/admin/catalog-token",{method:"POST",headers:headers(),body:JSON.stringify(body)});
    const data=await r.json();
    if(!r.ok) throw new Error(data?.error?.message||"Request failed");
    result.hidden=false; urlOut.value=new URL(data.url,location.href).toString(); openLink.href=urlOut.value; expiresOut.textContent=new Date(data.expiresAt).toLocaleString(); setState("Generated");
  }catch(err){showError(err.message||String(err))}
});
copy.addEventListener("click",async()=>{await navigator.clipboard.writeText(urlOut.value);setState("Copied")});
async function loadStats(){
  try{
    const r=await fetch("api/public/stats");
    const data=await r.json();
    if(!r.ok) throw new Error(data?.error?.message||"Stats unavailable");
    statsOut.innerHTML="";
    const counts=data.mediaTypeCounts||[];
    if(!counts.length){statsOut.innerHTML='<div><strong>'+Number(data.totalItems||0)+'</strong><span>Total items</span></div>';return}
    for(const item of counts){const div=document.createElement("div");div.innerHTML='<strong>'+Number(item.count||0)+'</strong><span>'+esc(item.mediaType||"items")+'</span>';statsOut.appendChild(div)}
  }catch(err){statsOut.innerHTML='<div><strong>!</strong><span>'+esc(err.message||"Unavailable")+'</span></div>'}
}
function esc(s){return String(s??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]))}
refreshStats.addEventListener("click",loadStats);
loadStats();
</script>
</body>
</html>`)
	}
}

func hostStats(ctx context.Context, d Deps, libraryIDs []string) (*runtimehost.CatalogStats, error) {
	if d.statsCache != nil {
		return d.statsCache.get(ctx, d.Host, libraryIDs, d.Sources)
	}
	host := currentHost(d)
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

	host := currentHost(Deps{Host: hostFn})
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

func currentHost(d Deps) Host {
	if d.Host == nil {
		return nil
	}
	return d.Host()
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
			continue
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
	return `:root{--bg:#0f1115;--fg:#f5f7fb;--muted:#aab3c2;--muted2:#8994a8;--panel:#171b23;--panel2:#151922;--border:#2b3342;--input:#171b23;--accent:#8eb6ff;--primary:#2563eb;--primary-border:#3b82f6}[data-theme="cinema-light"]{color-scheme:light;--bg:#f7f3ed;--fg:#201c18;--muted:#74675a;--muted2:#8c7b6a;--panel:#fffaf3;--panel2:#fffaf3;--border:#ded1c0;--input:#fffaf3;--accent:#9a3412;--primary:#9a3412;--primary-border:#c2410c}[data-theme="cobalt-studio"]{--bg:#101623;--fg:#eef4ff;--muted:#9fb2d0;--muted2:#8aa0c0;--panel:#172033;--panel2:#172033;--border:#2d3f61;--input:#121c2f;--accent:#60a5fa;--primary:#2563eb;--primary-border:#60a5fa}[data-theme="oxblood-noir"]{--bg:#170b10;--fg:#fff1f4;--muted:#c59aa6;--muted2:#a77d89;--panel:#241018;--panel2:#241018;--border:#4a2230;--input:#211018;--accent:#fb7185;--primary:#be123c;--primary-border:#fb7185}[data-theme="evergreen-studio"]{--bg:#0d1712;--fg:#ecfdf3;--muted:#9bc7b2;--muted2:#7da18f;--panel:#14241b;--panel2:#14241b;--border:#2b4b39;--input:#102017;--accent:#6ee7b7;--primary:#047857;--primary-border:#6ee7b7}body{margin:0;font-family:Inter,system-ui,-apple-system,Segoe UI,sans-serif;background:var(--bg);color:var(--fg)} .shell{max-width:1180px;margin:0 auto;padding:32px 20px}.hero{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;padding:48px 0}.eyebrow{color:var(--accent);text-transform:uppercase;font-size:12px;letter-spacing:.08em}.hero h1{font-size:44px;line-height:1.05;margin:0 0 12px}.lead{color:var(--muted);font-size:18px}.stat{border:1px solid var(--border);border-radius:8px;padding:20px;min-width:180px;background:var(--panel)}.stat span{display:block;font-size:36px;font-weight:700}.stat small{color:var(--muted)}.panel{border-top:1px solid var(--border);padding:28px 0}.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px}.stats div,.card{background:var(--panel);border:1px solid var(--border);border-radius:8px}.stats div{padding:16px}.stats strong{display:block;font-size:26px}.stats span,.card p{color:var(--muted)}.toolbar{display:grid;grid-template-columns:1fr 160px 160px;gap:10px;position:sticky;top:0;background:var(--bg);padding:16px 0}input,select,.button{border:1px solid var(--border);background:var(--input);color:var(--fg);border-radius:6px;padding:10px 12px}.button{cursor:pointer}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:14px}.poster{aspect-ratio:2/3;background:var(--panel);border-radius:8px 8px 0 0;overflow:hidden}.poster img{width:100%;height:100%;object-fit:cover}.body{padding:12px}.body h3{font-size:15px;margin:0 0 8px}@media(max-width:700px){.hero{display:block}.toolbar{grid-template-columns:1fr}.hero h1{font-size:34px}}`
}

func adminCSS() string {
	return `.admin-shell{max-width:1220px}.admin-hero{padding-bottom:24px}.admin-grid{display:grid;grid-template-columns:minmax(0,1fr) 360px;gap:24px}.admin-panel{border:1px solid var(--border);border-radius:8px;background:var(--panel2);padding:20px}.panel-head{display:flex;align-items:center;justify-content:space-between;gap:12px}.admin-panel h2{margin:0 0 16px}.admin-form{display:grid;gap:16px}.admin-form label{display:grid;gap:7px;color:var(--muted)}.admin-form fieldset{border:1px solid var(--border);border-radius:8px;margin:0;padding:14px;display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:10px}.admin-form legend{color:var(--muted);padding:0 6px}.check{display:flex!important;align-items:center;gap:8px}.check input{width:auto}.primary{background:var(--primary);border-color:var(--primary-border)}.status-pill{display:inline-flex;align-items:center;border:1px solid var(--primary-border);border-radius:999px;color:var(--accent);padding:6px 10px;font-size:12px}.status-pill.bad{border-color:#b54747;color:#ffadad}.result-box,.error-box{margin-top:18px}.result-box textarea{width:100%;box-sizing:border-box;resize:vertical}.actions{display:flex;gap:10px;margin-top:10px}.details{display:grid;grid-template-columns:130px 1fr;gap:10px;margin:0 0 16px}.details dt{color:var(--muted2)}.details dd{margin:0;color:var(--fg);overflow-wrap:anywhere}.compact{margin-top:12px}.error-box{white-space:pre-wrap;border:1px solid #6b2d2d;background:#2b1518;color:#ffc4c4;border-radius:8px;padding:12px}.slim{grid-template-columns:1fr 1fr}@media(max-width:900px){.admin-grid{grid-template-columns:1fr}.slim{grid-template-columns:repeat(auto-fit,minmax(140px,1fr))}}`
}

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
