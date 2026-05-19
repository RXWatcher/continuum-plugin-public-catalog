import { FormEvent, ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type Bootstrap = {
  mode: "landing" | "catalog" | "detail";
  theme: string;
  catalogHref: string;
  customHTML: string;
  authRequired: boolean;
  token: string;
  initialStats?: Stats;
  initialLibraryId?: string;
  initialItems?: CatalogItem[];
  initialNextPageToken?: string;
  initialTotalCount?: number;
  initialFilters?: CatalogFilters;
};

type Count = {
  libraryId?: string;
  mediaType?: string;
  libraryName?: string;
  label?: string;
  count: number;
};

type Stats = {
  totalItems: number;
  mediaTypeCounts?: Count[];
  libraryCounts?: Count[];
  qualityCounts?: Count[];
};

type CatalogItem = {
  mediaId?: string;
  MediaID?: string;
  title?: string;
  Title?: string;
  seriesTitle?: string;
  SeriesTitle?: string;
  mediaType?: string;
  MediaType?: string;
  type?: string;
  Type?: string;
  seasonNumber?: number;
  SeasonNumber?: number;
  episodeNumber?: number;
  EpisodeNumber?: number;
  year?: number;
  Year?: number;
  overview?: string;
  Overview?: string;
  posterUrl?: string;
  PosterURL?: string;
  poster_url?: string;
  backdropUrl?: string;
  BackdropURL?: string;
  genres?: string[];
  Genres?: string[];
  rating?: number;
  Rating?: number;
};

type CatalogResponse = {
  items?: CatalogItem[];
  Items?: CatalogItem[];
  nextPageToken?: string;
  NextPageToken?: string;
  totalCount?: number;
  TotalCount?: number;
};

type CatalogFilters = {
  genres?: string[];
  years?: number[];
  decades?: { label: string; yearMin: number; yearMax: number }[];
};

type Detail = {
  contentId: string;
  type: string;
  title: string;
  originalTitle?: string;
  seriesId?: string;
  seriesTitle?: string;
  seasonNumber?: number;
  episodeNumber?: number;
  episodeCount?: number;
  year?: number;
  overview?: string;
  tagline?: string;
  runtime?: number;
  contentRating?: string;
  genres?: string[];
  studios?: string[];
  networks?: string[];
  countries?: string[];
  firstAirDate?: string;
  lastAirDate?: string;
  releaseDate?: string;
  airDate?: string;
  isSpecials?: boolean;
  ratingImdb?: number;
  ratingTmdb?: number;
  ratingRtCritic?: number;
  ratingRtAudience?: number;
  posterUrl?: string;
  backdropUrl?: string;
  logoUrl?: string;
  seasonCount?: number;
  libraries?: { id: string; name: string; type: string }[];
  qualities?: { resolution?: string; videoCodec?: string; audioCodec?: string; container?: string; hdr?: boolean; count: number }[];
};

type Season = {
  contentId: string;
  seriesId: string;
  seasonNumber: number;
  title: string;
  overview?: string;
  airDate?: string;
  posterUrl?: string;
  episodeCount: number;
};

type Episode = {
  contentId: string;
  seriesId: string;
  seasonId?: string;
  seasonNumber: number;
  episodeNumber: number;
  title: string;
  overview?: string;
  airDate?: string;
  runtime?: number;
  ratingImdb?: number;
  ratingTmdb?: number;
  stillUrl?: string;
};

type CatalogBrowserState = {
  libraryId: string;
  q: string;
  type: string;
  sort: string;
  desc: boolean;
  genre: string;
  year: string;
  decade: string;
};

type LibraryMode = "movie" | "tv" | "mixed" | "unknown";

const PUBLIC_THEME_STORAGE_KEY = "continuum.publicCatalog.theme";
const DARK_THEME = "midnight-cinema";
const LIGHT_THEME = "cinema-light";
const supportedThemes = new Set([DARK_THEME, LIGHT_THEME, "cobalt-studio", "oxblood-noir", "evergreen-studio"]);

const mediaLabels: Record<string, string> = {
  movie: "Movie",
  tv: "Series",
  series: "Series",
  episode: "Episode",
  ebook: "Ebook",
  audiobook: "Audiobook"
};

const fallbackBootstrap: Bootstrap = {
  mode: "landing",
  theme: DARK_THEME,
  catalogHref: "catalog",
  customHTML: "",
  authRequired: false,
  token: ""
};

const defaultCatalogState: CatalogBrowserState = {
  libraryId: "",
  q: "",
  type: "",
  sort: "added_at",
  desc: true,
  genre: "",
  year: "",
  decade: ""
};

function readBootstrap(): Bootstrap {
  const node = document.getElementById("public-catalog-bootstrap");
  if (!node?.textContent) return fallbackBootstrap;
  try {
    return { ...fallbackBootstrap, ...JSON.parse(node.textContent) };
  } catch {
    return fallbackBootstrap;
  }
}

const bootstrap = readBootstrap();
const initialTheme = resolveThemePreference(bootstrap.theme);
document.documentElement.dataset.theme = initialTheme;

function App() {
  const [theme, setTheme] = useState(initialTheme);
  const [authorized, setAuthorized] = useState(!bootstrap.authRequired);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      window.localStorage.setItem(PUBLIC_THEME_STORAGE_KEY, theme);
    } catch {}
    const params = new URLSearchParams(window.location.search);
    if (params.get("theme") === theme) return;
    params.set("theme", theme);
    const next = `${window.location.pathname}${params.toString() ? `?${params.toString()}` : ""}`;
    window.history.replaceState(null, "", next);
  }, [theme]);

  if (!authorized) return <AuthPage onAuthorized={() => setAuthorized(true)} theme={theme} onThemeChange={setTheme} />;
  if (bootstrap.mode === "detail") return <DetailPage bootstrap={bootstrap} theme={theme} onThemeChange={setTheme} />;
  if (bootstrap.mode === "catalog") return <CatalogPage bootstrap={bootstrap} theme={theme} onThemeChange={setTheme} />;
  return <LandingPage bootstrap={bootstrap} theme={theme} onThemeChange={setTheme} />;
}

function AuthPage({ onAuthorized, theme, onThemeChange }: { onAuthorized: () => void; theme: string; onThemeChange: (theme: string) => void }) {
  const [passwordError, setPasswordError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setPasswordError("");
    try {
      const res = await fetch(apiPath("api/public/catalog-login"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ password: String(form.get("password") || "") })
      });
      const data = await readJSON<Record<string, unknown>>(res);
      if (!res.ok) throw new Error(errorMessage(data) || "Password rejected");
      onAuthorized();
    } catch (err) {
      setPasswordError(err instanceof Error ? err.message : "Password rejected");
    }
  }

  return (
    <main className="app-shell auth-shell">
      <TopBar theme={theme} onThemeChange={onThemeChange} />
      <section className="auth-panel">
        <p className="eyebrow">Public catalog</p>
        <h1>Catalog access</h1>
        <p className="lead">Enter the catalog password to browse the public read-only library.</p>
        <form className="auth-form" onSubmit={submit}>
          <label>
            Password
            <input name="password" type="password" autoFocus />
          </label>
          <button className="button primary" type="submit">
            Open catalog
          </button>
        </form>
        {passwordError ? <p className="inline-error">{passwordError}</p> : null}
      </section>
    </main>
  );
}

function LandingPage({ bootstrap, theme, onThemeChange }: { bootstrap: Bootstrap; theme: string; onThemeChange: (theme: string) => void }) {
  const { stats, status } = useStats(bootstrap.initialStats || null);
  const libraries = (stats?.libraryCounts || []).filter((library) => library.count > 0);
  const total = stats?.totalItems ?? 0;
  const firstLibraryHref = libraries[0]?.libraryId ? `catalog?libraryId=${encodeURIComponent(String(libraries[0].libraryId))}` : "catalog";
  const browseHref = bootstrap.catalogHref && bootstrap.catalogHref !== "catalog" ? bootstrap.catalogHref : firstLibraryHref;

  return (
    <main className="app-shell">
      <TopBar theme={theme} onThemeChange={onThemeChange} />
      <section className="home-layout">
        <div className="home-copy">
          <p className="eyebrow">Public catalog</p>
          <h1>Libraries</h1>
          <p className="lead">Browse the same library structure you see in Continuum, with read-only detail pages and scoped public access.</p>
          <div className="action-row">
            <a className="button primary" href={withToken(publicRoute(browseHref), bootstrap.token)}>
              Browse libraries
            </a>
            <a className="button secondary" href="#public-note">
              Public note
            </a>
          </div>
        </div>
        <aside className="summary-panel" aria-label="Catalog summary">
          <Metric label="Total" value={total} caption="Items currently available" />
          <div className="summary-lines">
            {(stats?.mediaTypeCounts || []).map((count) => (
              <div key={count.mediaType}>
                <span>{mediaLabels[count.mediaType || ""] || count.mediaType}</span>
                <strong>{formatCount(count.count)}</strong>
              </div>
            ))}
          </div>
        </aside>
      </section>

      <section className="section">
        <SectionHeading eyebrow="Libraries" title="Browse by library" aside={status === "loading" ? "Loading stats" : `${formatCount(libraries.length)} libraries`} />
        <div className="library-grid">
          {libraries.map((library) => (
            <a
              className="library-card"
              href={withToken(publicRoute(`catalog?libraryId=${encodeURIComponent(String(library.libraryId || ""))}`), bootstrap.token)}
              key={library.libraryId || library.libraryName}
            >
              <span>{mediaLabels[library.mediaType || ""] || library.mediaType || "Library"}</span>
              <strong>{library.libraryName || "Library"}</strong>
              <small>{formatCount(library.count)} total items</small>
            </a>
          ))}
        </div>
      </section>

      <StatsSection stats={stats} status={status} />

      <section id="public-note" className="section custom-section">
        <SectionHeading eyebrow="Custom HTML" title="Published page section" />
        <div className="published-html" dangerouslySetInnerHTML={{ __html: bootstrap.customHTML || sampleCustomHTML }} />
      </section>
    </main>
  );
}

function CatalogPage({ bootstrap, theme, onThemeChange }: { bootstrap: Bootstrap; theme: string; onThemeChange: (theme: string) => void }) {
  return (
    <main className="app-shell catalog-shell">
      <TopBar theme={theme} onThemeChange={onThemeChange} />
      <CatalogBrowser bootstrap={bootstrap} />
    </main>
  );
}

function CatalogBrowser({ bootstrap }: { bootstrap: Bootstrap }) {
  const { stats } = useStats(bootstrap.initialStats || null);
  const libraries = useMemo(() => (stats?.libraryCounts || []).filter((library) => library.count > 0), [stats]);
  const [browser, setBrowser] = useState<CatalogBrowserState>(() => readCatalogState(bootstrap.initialLibraryId || ""));
  const [filtersData, setFiltersData] = useState<CatalogFilters>(bootstrap.initialFilters || {});
  const [items, setItems] = useState<CatalogItem[]>(bootstrap.initialItems || []);
  const [nextPageToken, setNextPageToken] = useState(bootstrap.initialNextPageToken || "");
  const [totalCount, setTotalCount] = useState(bootstrap.initialTotalCount || bootstrap.initialItems?.length || 0);
  const [loading, setLoading] = useState((bootstrap.initialItems || []).length === 0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const requestSeq = useRef(0);
  const pageCache = useRef<Map<string, CatalogResponse>>(new Map());
  const filterCache = useRef<Map<string, CatalogFilters>>(new Map());
  const initialSeeded = useRef(false);
  const debouncedQuery = useDebouncedValue(browser.q, 180);

  useEffect(() => {
    if (initialSeeded.current) return;
    initialSeeded.current = true;
    if (bootstrap.initialItems?.length && bootstrap.initialLibraryId) {
      const seededType = readCatalogState(bootstrap.initialLibraryId).type;
      pageCache.current.set(
        catalogStateKey({ ...defaultCatalogState, libraryId: bootstrap.initialLibraryId, type: seededType }),
        {
          items: bootstrap.initialItems,
          nextPageToken: bootstrap.initialNextPageToken,
          totalCount: bootstrap.initialTotalCount
        }
      );
    }
    if (bootstrap.initialFilters && bootstrap.initialLibraryId) {
      const seededType = readCatalogState(bootstrap.initialLibraryId).type;
      filterCache.current.set(catalogFilterKey(bootstrap.initialLibraryId, seededType), bootstrap.initialFilters);
    }
  }, [bootstrap.initialFilters, bootstrap.initialItems, bootstrap.initialLibraryId, bootstrap.initialNextPageToken, bootstrap.initialTotalCount]);

  useEffect(() => {
    function handlePopState() {
      setBrowser(readCatalogState(bootstrap.initialLibraryId || ""));
    }
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, [bootstrap.initialLibraryId]);

  const activeLibrary = useMemo(() => libraries.find((library) => String(library.libraryId || "") === browser.libraryId) || null, [browser.libraryId, libraries]);
  const libraryMode = normalizeLibraryMode(activeLibrary?.mediaType || "");

  useEffect(() => {
    if (!libraries.length) return;
    const canonical = canonicalCatalogState(browser, libraries, bootstrap.initialLibraryId || "");
    if (!sameCatalogState(canonical, browser)) {
      setBrowser(canonical);
      writeCatalogState(canonical, bootstrap.token, true);
    }
  }, [bootstrap.initialLibraryId, bootstrap.token, browser, libraries]);

  const effectiveState = useMemo(() => ({ ...browser, q: debouncedQuery }), [browser, debouncedQuery]);
  const typeOptions = useMemo(() => catalogTypeOptions(libraryMode), [libraryMode]);
  const resultKey = useMemo(() => catalogStateKey(effectiveState), [effectiveState]);

  useEffect(() => {
    if (!effectiveState.libraryId) return;
    const cacheKey = catalogFilterKey(effectiveState.libraryId, effectiveState.type);
    const cached = filterCache.current.get(cacheKey);
    if (cached) {
      setFiltersData(cached);
      return;
    }
    const controller = new AbortController();
    const params = new URLSearchParams();
    params.set("library_id", effectiveState.libraryId);
    if (bootstrap.token) params.set("token", bootstrap.token);
    if (effectiveState.type) params.set("media_type", effectiveState.type);
    fetch(apiPath(`api/catalog/filters?${params.toString()}`), { signal: controller.signal })
      .then((res) => readJSON<CatalogFilters>(res).then((data) => ({ res, data })))
      .then(({ res, data }) => {
        if (!res.ok) throw new Error(errorMessage(data) || "Filters unavailable");
        filterCache.current.set(cacheKey, data);
        setFiltersData(data);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        console.error(err);
        setFiltersData({});
      });
    return () => controller.abort();
  }, [bootstrap.token, effectiveState.libraryId, effectiveState.type]);

  useEffect(() => {
    if (!effectiveState.libraryId) return;
    const cached = pageCache.current.get(resultKey);
    if (cached) {
      const normalized = normalizeCatalogResponse(cached);
      setItems(normalized.items);
      setNextPageToken(normalized.nextPageToken);
      setTotalCount(normalized.totalCount || normalized.items.length);
      setLoading(false);
      setLoadingMore(false);
      setError("");
      return;
    }
    const requestID = ++requestSeq.current;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    const params = buildCatalogParams(effectiveState, bootstrap.token);
    fetch(apiPath(`api/catalog/media?${params.toString()}`), { signal: controller.signal })
      .then((res) => readJSON<CatalogResponse>(res).then((data) => ({ res, data })))
      .then(({ res, data }) => {
        if (!res.ok) throw new Error(errorMessage(data) || "Catalog unavailable");
        if (requestID !== requestSeq.current) return;
        const normalized = normalizeCatalogResponse(data);
        pageCache.current.set(resultKey, normalized);
        setItems(normalized.items);
        setNextPageToken(normalized.nextPageToken);
        setTotalCount(normalized.totalCount || normalized.items.length);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted || requestID !== requestSeq.current) return;
        setItems([]);
        setNextPageToken("");
        setTotalCount(0);
        setError(err instanceof Error ? err.message : "Catalog unavailable");
      })
      .finally(() => {
        if (requestID === requestSeq.current) {
          setLoading(false);
          setLoadingMore(false);
        }
      });
    return () => controller.abort();
  }, [bootstrap.token, effectiveState, resultKey]);

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !nextPageToken || loading || loadingMore || !effectiveState.libraryId) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) return;
        void loadMore();
      },
      { rootMargin: "1200px 0px" }
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [effectiveState, loading, loadingMore, nextPageToken]);

  function commitBrowser(next: CatalogBrowserState, replace: boolean) {
    setBrowser(next);
    writeCatalogState(next, bootstrap.token, replace);
  }

  function patchBrowser(patch: Partial<CatalogBrowserState>, replace = true) {
    commitBrowser({ ...browser, ...patch }, replace);
  }

  function selectLibrary(libraryId: string) {
    const nextLibrary = libraries.find((library) => String(library.libraryId || "") === libraryId) || null;
    const nextType = defaultTypeForLibrary(nextLibrary?.mediaType || "");
    commitBrowser(
      {
        ...browser,
        libraryId,
        type: nextType,
        genre: "",
        year: "",
        decade: ""
      },
      false
    );
  }

  async function loadMore() {
    if (!nextPageToken || loading || loadingMore || !effectiveState.libraryId) return;
    const requestID = ++requestSeq.current;
    const controller = new AbortController();
    setLoadingMore(true);
    setError("");
    const params = buildCatalogParams(effectiveState, bootstrap.token);
    params.set("page_token", nextPageToken);
    try {
      const res = await fetch(apiPath(`api/catalog/media?${params.toString()}`), { signal: controller.signal });
      const data = await readJSON<CatalogResponse>(res);
      if (!res.ok) throw new Error(errorMessage(data) || "Catalog unavailable");
      if (requestID !== requestSeq.current) return;
      const normalized = normalizeCatalogResponse(data);
      setItems((current) => [...current, ...normalized.items]);
      setNextPageToken(normalized.nextPageToken);
      setTotalCount(normalized.totalCount || totalCount);
    } catch (err) {
      if (!controller.signal.aborted && requestID === requestSeq.current) {
        setError(err instanceof Error ? err.message : "Catalog unavailable");
      }
    } finally {
      if (requestID === requestSeq.current) {
        setLoadingMore(false);
      }
      controller.abort();
    }
  }

  const shownCount = items.length;
  const total = totalCount || activeLibrary?.count || 0;

  return (
    <div className="browse-workspace">
      <aside className="library-rail" aria-label="Libraries">
        <div className="rail-heading">
          <span>Libraries</span>
          <strong>{formatCount(libraries.length)}</strong>
        </div>
        {libraries.map((library) => {
          const id = String(library.libraryId || "");
          const active = id === browser.libraryId;
          return (
            <button className={`library-row ${active ? "active" : ""}`} onClick={() => selectLibrary(id)} type="button" key={id || library.libraryName}>
              <span className="library-icon">{libraryInitials(library.libraryName || "Library")}</span>
              <span>
                <strong>{library.libraryName || "Library"}</strong>
                <small>{formatCount(library.count)} items · {mediaLabels[library.mediaType || ""] || library.mediaType || "Library"}</small>
              </span>
            </button>
          );
        })}
      </aside>

      <div className="library-stage">
        <header className="browse-header">
          <div>
            <p className="eyebrow">{mediaLabels[activeLibrary?.mediaType || ""] || activeLibrary?.mediaType || "Library"}</p>
            <h1>{activeLibrary?.libraryName || "Library"}</h1>
          </div>
          <div className="browse-total">
            <span>Total items</span>
            <strong>{formatCount(total)}</strong>
          </div>
        </header>

        <section className="filter-panel" aria-label="Catalog filters">
          <label className="search-field">
            <span>Search</span>
            <input value={browser.q} onChange={(event) => patchBrowser({ q: event.target.value })} type="search" placeholder="Title, original title, series" autoComplete="off" />
          </label>
          {typeOptions.length > 0 ? (
            <label>
              <span>Browse</span>
              <select value={browser.type} onChange={(event) => patchBrowser({ type: event.target.value, genre: "", year: "", decade: "" })}>
                {typeOptions.map((option) => (
                  <option value={option.value} key={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label>
            <span>Sort</span>
            <select value={browser.sort} onChange={(event) => patchBrowser({ sort: event.target.value })}>
              <option value="added_at">Recently added</option>
              <option value="title">Title</option>
              <option value="year">Year</option>
              <option value="rating">Rating</option>
            </select>
          </label>
          <label>
            <span>Order</span>
            <select value={browser.desc ? "true" : "false"} onChange={(event) => patchBrowser({ desc: event.target.value === "true" })}>
              <option value="true">Descending</option>
              <option value="false">Ascending</option>
            </select>
          </label>
          <label>
            <span>Genre</span>
            <select value={browser.genre} onChange={(event) => patchBrowser({ genre: event.target.value })}>
              <option value="">All genres</option>
              {(filtersData.genres || []).map((item) => (
                <option value={item} key={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Decade</span>
            <select value={browser.decade} onChange={(event) => patchBrowser({ decade: event.target.value, year: "" })}>
              <option value="">All decades</option>
              {(filtersData.decades || []).map((item) => (
                <option value={item.label} key={item.label}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Year</span>
            <select value={browser.year} onChange={(event) => patchBrowser({ year: event.target.value, decade: "" })}>
              <option value="">All years</option>
              {(filtersData.years || []).map((item) => (
                <option value={String(item)} key={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
        </section>

        <div className="result-strip">
          <strong>{loading && !items.length ? "Loading library" : `${formatCount(shownCount)} shown`}</strong>
          <span>{total ? `${formatCount(total)} total in this library` : "Public catalog"}</span>
        </div>

        <section id="results" className="poster-grid" aria-live="polite">
          {items.map((item, index) => (
            <MediaCard item={item} token={bootstrap.token} libraryId={browser.libraryId} key={`${normalizedID(item)}-${index}`} />
          ))}
          {loading && !items.length ? <PosterSkeletons /> : null}
          {!items.length && !loading ? <div className="empty-state">{error || "No items found."}</div> : null}
        </section>
        {error && items.length ? <p className="inline-error">{error}</p> : null}
        <div className="load-row">
          {nextPageToken ? <span className="end-marker">{loadingMore ? "Loading more" : "Scroll for more"}</span> : shownCount ? <span className="end-marker">End of library view</span> : null}
          <div ref={loadMoreRef} />
        </div>
      </div>
    </div>
  );
}

function DetailPage({ bootstrap, theme, onThemeChange }: { bootstrap: Bootstrap; theme: string; onThemeChange: (theme: string) => void }) {
  const id = decodeURIComponent(window.location.pathname.split("/item/")[1] || "");
  const params = new URLSearchParams(window.location.search);
  const libraryId = params.get("libraryId") || "";
  const [detail, setDetail] = useState<Detail | null>(null);
  const [seasons, setSeasons] = useState<Season[]>([]);
  const [episodesBySeason, setEpisodesBySeason] = useState<Record<number, Episode[]>>({});
  const [activeSeason, setActiveSeason] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    async function load() {
      setLoading(true);
      setError("");
      try {
        const itemRes = await fetch(apiPath(`api/catalog/items/${encodeURIComponent(id)}${tokenQuery(bootstrap.token)}`));
        const item = await readJSON<Detail>(itemRes);
        if (!itemRes.ok) throw new Error(errorMessage(item) || "Item not found");
        if (cancelled) return;
        setDetail(item);
        const seriesID = item.type === "series" ? item.contentId : item.seriesId || "";
        if (seriesID) {
          const seasonRes = await fetch(apiPath(`api/catalog/items/${encodeURIComponent(seriesID)}/seasons${tokenQuery(bootstrap.token)}`));
          const seasonData = await readJSON<Season[]>(seasonRes);
          if (!seasonRes.ok) throw new Error(errorMessage(seasonData) || "Season data unavailable");
          if (cancelled) return;
          const nextSeasons = Array.isArray(seasonData) ? seasonData : [];
          setSeasons(nextSeasons);
          if (item.type === "series") {
            const preferred = nextSeasons.find((season) => season.seasonNumber > 0) || nextSeasons[0] || null;
            setActiveSeason(preferred ? preferred.seasonNumber : null);
          } else if (item.seasonNumber != null) {
            setActiveSeason(item.seasonNumber);
          } else {
            setActiveSeason(null);
          }
        } else {
          setSeasons([]);
          setActiveSeason(null);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : "Item not found");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [bootstrap.token, id]);

  useEffect(() => {
    if (!detail || activeSeason == null || episodesBySeason[activeSeason]) return;
    const seriesID = detail.type === "series" ? detail.contentId : detail.seriesId || "";
    if (!seriesID) return;
    let cancelled = false;
    async function loadEpisodes() {
      const res = await fetch(apiPath(`api/catalog/series/${encodeURIComponent(seriesID)}/seasons/${activeSeason}/episodes${tokenQuery(bootstrap.token)}`));
      const data = await readJSON<Episode[]>(res);
      if (!cancelled && res.ok) {
        setEpisodesBySeason((current) => ({ ...current, [activeSeason]: Array.isArray(data) ? data : [] }));
      }
    }
    void loadEpisodes();
    return () => {
      cancelled = true;
    };
  }, [activeSeason, bootstrap.token, detail, episodesBySeason]);

  if (loading) {
    return (
      <main className="app-shell">
        <TopBar theme={theme} onThemeChange={onThemeChange} />
        <div className="detail-skeleton" />
      </main>
    );
  }

  if (error || !detail) {
    return (
      <main className="app-shell">
        <TopBar theme={theme} onThemeChange={onThemeChange} />
        <section className="empty-state detail-empty">{error || "Item not found."}</section>
      </main>
    );
  }

  const backHref = withToken(publicRoute(libraryId ? `catalog?libraryId=${encodeURIComponent(libraryId)}` : "catalog"), bootstrap.token);
  const seasonEpisodes = activeSeason == null ? [] : episodesBySeason[activeSeason] || [];
  const currentEpisode = detail.type === "episode" ? seasonEpisodes.find((episode) => episode.contentId === detail.contentId) || null : null;
  const currentEpisodeIndex = currentEpisode ? seasonEpisodes.findIndex((episode) => episode.contentId === currentEpisode.contentId) : -1;
  const previousEpisode = currentEpisodeIndex > 0 ? seasonEpisodes[currentEpisodeIndex - 1] : null;
  const nextEpisode = currentEpisodeIndex >= 0 && currentEpisodeIndex < seasonEpisodes.length - 1 ? seasonEpisodes[currentEpisodeIndex + 1] : null;

  return (
    <main className="detail-shell">
      <TopBar theme={theme} onThemeChange={onThemeChange} />
      {detail.type === "movie" ? (
        <MovieDetail detail={detail} backHref={backHref} />
      ) : detail.type === "series" ? (
        <SeriesDetail detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={seasonEpisodes} onSeasonChange={setActiveSeason} backHref={backHref} token={bootstrap.token} libraryId={libraryId} />
      ) : detail.type === "season" ? (
        <SeasonDetail detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={seasonEpisodes} onSeasonChange={setActiveSeason} backHref={backHref} token={bootstrap.token} libraryId={libraryId} />
      ) : (
        <EpisodeDetail detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={seasonEpisodes} onSeasonChange={setActiveSeason} backHref={backHref} token={bootstrap.token} libraryId={libraryId} previousEpisode={previousEpisode} nextEpisode={nextEpisode} />
      )}
    </main>
  );
}

function MovieDetail({ detail, backHref }: { detail: Detail; backHref: string }) {
  return (
    <>
      <DetailHeroSection detail={detail} backHref={backHref} eyebrow="Movie" />
      <DetailContentArea side={<AvailabilityPanel detail={detail} />} main={<MetadataPanel detail={detail} />} />
    </>
  );
}

function SeriesDetail({
  detail,
  seasons,
  activeSeason,
  episodes,
  onSeasonChange,
  backHref,
  token,
  libraryId
}: {
  detail: Detail;
  seasons: Season[];
  activeSeason: number | null;
  episodes: Episode[];
  onSeasonChange: (season: number) => void;
  backHref: string;
  token: string;
  libraryId: string;
}) {
  return (
    <>
      <DetailHeroSection detail={detail} backHref={backHref} eyebrow="Series" />
      <DetailContentArea
        side={<AvailabilityPanel detail={detail} />}
        main={
          <>
            <MetadataPanel detail={detail} />
            <SeasonPanel detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={episodes} onSeasonChange={onSeasonChange} token={token} libraryId={libraryId} interactive />
          </>
        }
      />
    </>
  );
}

function SeasonDetail({
  detail,
  seasons,
  activeSeason,
  episodes,
  onSeasonChange,
  backHref,
  token,
  libraryId
}: {
  detail: Detail;
  seasons: Season[];
  activeSeason: number | null;
  episodes: Episode[];
  onSeasonChange: (season: number) => void;
  backHref: string;
  token: string;
  libraryId: string;
}) {
  const context = detail.seriesTitle ? [detail.seriesTitle, seasonLabel(detail.seasonNumber || 0, detail.title, detail.isSpecials || false)].join(" / ") : seasonLabel(detail.seasonNumber || 0, detail.title, detail.isSpecials || false);
  return (
    <>
      <DetailHeroSection detail={detail} backHref={backHref} eyebrow="Season" context={context} compact />
      <DetailContentArea
        side={<AvailabilityPanel detail={detail} />}
        main={
          <>
            <MetadataPanel detail={detail} />
            <SeasonPanel detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={episodes} onSeasonChange={onSeasonChange} token={token} libraryId={libraryId} interactive={false} />
          </>
        }
      />
    </>
  );
}

function EpisodeDetail({
  detail,
  seasons,
  activeSeason,
  episodes,
  onSeasonChange,
  backHref,
  token,
  libraryId,
  previousEpisode,
  nextEpisode
}: {
  detail: Detail;
  seasons: Season[];
  activeSeason: number | null;
  episodes: Episode[];
  onSeasonChange: (season: number) => void;
  backHref: string;
  token: string;
  libraryId: string;
  previousEpisode: Episode | null;
  nextEpisode: Episode | null;
}) {
  const contextParts = [detail.seriesTitle || "", seasonLabel(detail.seasonNumber || 0, "", detail.isSpecials || false), detail.episodeNumber ? `Episode ${detail.episodeNumber}` : ""].filter(Boolean);
  return (
    <>
      <DetailHeroSection detail={detail} backHref={backHref} eyebrow="Episode" context={contextParts.join(" / ")} compact />
      <DetailContentArea
        side={
          <>
            <AvailabilityPanel detail={detail} />
            <EpisodeNavigationPanel previousEpisode={previousEpisode} nextEpisode={nextEpisode} token={token} libraryId={libraryId} />
          </>
        }
        main={
          <>
            <MetadataPanel detail={detail} />
            <SeasonPanel detail={detail} seasons={seasons} activeSeason={activeSeason} episodes={episodes} onSeasonChange={onSeasonChange} token={token} libraryId={libraryId} interactive={false} />
          </>
        }
      />
    </>
  );
}

function DetailHeroSection({
  detail,
  backHref,
  eyebrow,
  context,
  compact = false
}: {
  detail: Detail;
  backHref: string;
  eyebrow: string;
  context?: string;
  compact?: boolean;
}) {
  const meta = detailPills(detail);
  const title = detail.type === "season" && detail.seriesTitle ? `${detail.seriesTitle}: ${seasonLabel(detail.seasonNumber || 0, detail.title, detail.isSpecials || false)}` : detail.title;
  return (
    <section className={`detail-hero ${compact ? "detail-hero-compact" : ""}`}>
      {detail.backdropUrl ? <img className="detail-backdrop" src={detail.backdropUrl} alt="" /> : null}
      <div className="detail-shade" />
      <div className="detail-content">
        {detail.posterUrl ? (
          <div className="detail-poster">
            <img src={detail.posterUrl} alt={detail.title} />
          </div>
        ) : null}
        <div className="detail-copy">
          <a className="back-link" href={backHref}>
            Back to library
          </a>
          <p className="eyebrow">{eyebrow}</p>
          {context ? <p className="detail-context">{context}</p> : null}
          <h1>{title}</h1>
          {detail.tagline ? <p className="tagline">{detail.tagline}</p> : null}
          <div className="detail-meta">
            {meta.map((item) => (
              <span key={item}>{item}</span>
            ))}
          </div>
          {detail.overview ? <p className="overview">{detail.overview}</p> : null}
          <div className="detail-actions-note">Read-only public catalog. Playback and downloads stay in Continuum.</div>
        </div>
      </div>
    </section>
  );
}

function DetailContentArea({ side, main }: { side: ReactNode; main: ReactNode }) {
  return (
    <section className="detail-body">
      <aside className="detail-facts">{side}</aside>
      <div className="detail-main">{main}</div>
    </section>
  );
}

function AvailabilityPanel({ detail }: { detail: Detail }) {
  return (
    <>
      <SectionHeading eyebrow="Availability" title="Libraries" />
      <div className="fact-list">
        {(detail.libraries || []).map((library) => (
          <div key={library.id}>
            <strong>{library.name}</strong>
            <span>{library.type}</span>
          </div>
        ))}
      </div>
      {detail.qualities?.length ? (
        <>
          <SectionHeading eyebrow="Files" title="Available quality" />
          <div className="quality-list">
            {detail.qualities.map((quality, index) => (
              <span key={`${quality.resolution}-${quality.videoCodec}-${index}`}>{qualityLabel(quality)}</span>
            ))}
          </div>
        </>
      ) : null}
    </>
  );
}

function MetadataPanel({ detail }: { detail: Detail }) {
  return (
    <>
      <SectionHeading eyebrow="Metadata" title="Details" />
      <div className="metadata-grid">
        <Meta label="Genres" value={(detail.genres || []).join(", ")} />
        <Meta label="Studios" value={(detail.studios || detail.networks || []).join(", ")} />
        <Meta label="Released" value={detail.releaseDate || detail.airDate || detail.firstAirDate || ""} />
        <Meta label="IMDb" value={detail.ratingImdb ? detail.ratingImdb.toFixed(1) : ""} />
        <Meta label="TMDb" value={detail.ratingTmdb ? detail.ratingTmdb.toFixed(1) : ""} />
        <Meta label="Countries" value={(detail.countries || []).join(", ")} />
      </div>
    </>
  );
}

function SeasonPanel({
  detail,
  seasons,
  activeSeason,
  episodes,
  onSeasonChange,
  token,
  libraryId,
  interactive
}: {
  detail: Detail;
  seasons: Season[];
  activeSeason: number | null;
  episodes: Episode[];
  onSeasonChange: (season: number) => void;
  token: string;
  libraryId: string;
  interactive: boolean;
}) {
  return (
    <section className="series-section">
      <SectionHeading eyebrow="Browse" title="Seasons and episodes" aside={seasons.length ? `${formatCount(seasons.length)} seasons` : undefined} />
      <div className="season-tabs">
        {seasons.map((season) =>
          interactive ? (
            <button className={season.seasonNumber === activeSeason ? "active" : ""} onClick={() => onSeasonChange(season.seasonNumber)} key={season.contentId} type="button">
              {seasonLabel(season.seasonNumber, season.title, season.seasonNumber === 0)}
              <span>{season.episodeCount} episodes</span>
            </button>
          ) : (
            <a className={`season-link ${season.seasonNumber === activeSeason ? "active" : ""}`} href={buildItemHref(season.contentId, token, libraryId)} key={season.contentId}>
              {seasonLabel(season.seasonNumber, season.title, season.seasonNumber === 0)}
              <span>{season.episodeCount} episodes</span>
            </a>
          )
        )}
      </div>
      <div className="episode-list">
        {episodes.map((episode) => (
          <a className="episode-row" href={buildItemHref(episode.contentId, token, libraryId)} key={episode.contentId}>
            <span className="episode-number">{episode.episodeNumber}</span>
            <div>
              <strong>{episode.title}</strong>
              <p>{episode.overview || "No episode overview is available."}</p>
            </div>
            <small>{episode.runtime ? formatRuntime(episode.runtime) : episode.airDate || ""}</small>
          </a>
        ))}
        {!episodes.length ? <div className="empty-state">Select a season to view episodes.</div> : null}
      </div>
      {detail.type === "episode" && detail.seriesTitle ? <p className="detail-context subtle-context">{detail.seriesTitle}</p> : null}
    </section>
  );
}

function EpisodeNavigationPanel({
  previousEpisode,
  nextEpisode,
  token,
  libraryId
}: {
  previousEpisode: Episode | null;
  nextEpisode: Episode | null;
  token: string;
  libraryId: string;
}) {
  if (!previousEpisode && !nextEpisode) return null;
  return (
    <>
      <SectionHeading eyebrow="Navigation" title="Episode flow" />
      <div className="fact-list">
        {previousEpisode ? (
          <a className="episode-nav" href={buildItemHref(previousEpisode.contentId, token, libraryId)}>
            <strong>Previous</strong>
            <span>{previousEpisode.title}</span>
          </a>
        ) : null}
        {nextEpisode ? (
          <a className="episode-nav" href={buildItemHref(nextEpisode.contentId, token, libraryId)}>
            <strong>Next</strong>
            <span>{nextEpisode.title}</span>
          </a>
        ) : null}
      </div>
    </>
  );
}

function StatsSection({ stats, status }: { stats: Stats | null; status: string }) {
  const groups = stats ? statGroups(stats) : [];
  return (
    <section className="section stats-section">
      <SectionHeading eyebrow="Stats" title="Availability totals" aside={status === "error" ? "Unavailable" : undefined} />
      <div className="stats-grid">
        {stats ? <StatCard kind="Total" label="Total items" count={stats.totalItems} /> : <StatSkeleton />}
        {groups.map((group) => (
          <StatCard key={`${group.kind}-${group.label}`} {...group} />
        ))}
      </div>
    </section>
  );
}

function TopBar({ theme, onThemeChange }: { theme: string; onThemeChange: (theme: string) => void }) {
  const params = new URLSearchParams(window.location.search);
  const libraryId = params.get("libraryId") || "";
  const browseHref = libraryId ? `catalog?libraryId=${encodeURIComponent(libraryId)}` : "catalog";
  const darkActive = theme !== LIGHT_THEME;
  return (
    <nav className="top-bar" aria-label="Public catalog">
      <a className="brand" href={withToken(publicRoute(""), bootstrap.token)}>
        Continuum Library
      </a>
      <div className="top-bar-links">
        <a href={withToken(publicRoute(""), bootstrap.token)}>Home</a>
        <a href={withToken(publicRoute(browseHref), bootstrap.token)}>Browse</a>
      </div>
      <div className="top-bar-controls">
        <div className="theme-toggle" role="group" aria-label="Theme mode">
          <button className={darkActive ? "active" : ""} onClick={() => onThemeChange(DARK_THEME)} type="button" aria-pressed={darkActive}>
            Dark
          </button>
          <button className={!darkActive ? "active" : ""} onClick={() => onThemeChange(LIGHT_THEME)} type="button" aria-pressed={!darkActive}>
            Light
          </button>
        </div>
      </div>
    </nav>
  );
}

function SectionHeading({ eyebrow, title, aside }: { eyebrow: string; title: string; aside?: string }) {
  return (
    <div className="section-heading">
      <div>
        <p className="eyebrow">{eyebrow}</p>
        <h2>{title}</h2>
      </div>
      {aside ? <span>{aside}</span> : null}
    </div>
  );
}

function StatCard({ kind, label, count }: { kind: string; label: string; count: number }) {
  return (
    <article className={`stat-card ${kind === "Library" ? "library-stat" : ""}`}>
      <small>{kind}</small>
      <strong>{formatCount(count)}</strong>
      <span>{label}</span>
    </article>
  );
}

function StatSkeleton() {
  return (
    <article className="stat-card skeleton">
      <small>Loading</small>
      <strong>...</strong>
      <span>Total items</span>
    </article>
  );
}

function PosterSkeletons() {
  return (
    <>
      {Array.from({ length: 18 }).map((_, index) => (
        <div className="media-card skeleton-card" key={index}>
          <div className="media-card-image" />
          <div className="media-card-copy">
            <span />
            <small />
          </div>
        </div>
      ))}
    </>
  );
}

function Metric({ label, value, caption }: { label: string; value: number; caption: string }) {
  return (
    <div className="metric">
      <small>{label}</small>
      <strong>{formatCount(value)}</strong>
      <span>{caption}</span>
    </div>
  );
}

function MediaCard({ item, token, libraryId }: { item: CatalogItem; token: string; libraryId: string }) {
  const title = normalizedTitle(item);
  const mediaType = normalizedMediaType(item);
  const posterUrl = item.posterUrl || item.PosterURL || item.poster_url || "";
  const backdropUrl = item.backdropUrl || item.BackdropURL || "";
  const cardImageUrl = posterUrl || backdropUrl;
  const imageClassName = posterUrl ? "" : backdropUrl ? "card-image-backdrop" : "";
  const year = item.year || item.Year || "";
  const genres = item.genres || item.Genres || [];
  const seriesTitle = normalizedSeriesTitle(item);
  const seasonNumber = item.seasonNumber || item.SeasonNumber || 0;
  const episodeNumber = item.episodeNumber || item.EpisodeNumber || 0;
  const episodeBadge = mediaType === "episode" ? `S${seasonNumber || 0} · E${episodeNumber || 0}` : "";
  const meta = [mediaLabels[mediaType] || mediaType, year].filter(Boolean).join(" · ");
  const subline = mediaType === "episode" ? [seriesTitle, episodeBadge].filter(Boolean).join(" · ") : genres.slice(0, 3).join(", ");
  const id = normalizedID(item);

  return (
    <a className="media-card" href={buildItemHref(id, token, libraryId)}>
      <div className="media-card-image">
        {cardImageUrl ? <img className={imageClassName} src={cardImageUrl} alt={title} loading="lazy" /> : <div className="poster-fallback">{title}</div>}
        <div className="poster-gradient" />
      </div>
      <div className="media-card-copy">
        {seriesTitle && mediaType === "episode" ? <p className="media-card-series">{seriesTitle}</p> : null}
        <h3>{title}</h3>
        <p className="meta">{meta}</p>
        <p>{subline}</p>
      </div>
    </a>
  );
}

function Meta({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function useStats(initialStats: Stats | null) {
  const [stats, setStats] = useState<Stats | null>(initialStats);
  const [status, setStatus] = useState(initialStats ? "ready" : "loading");

  useEffect(() => {
    if (initialStats) return;
    let cancelled = false;
    fetch(apiPath("api/public/stats"))
      .then((res) => readJSON<Stats>(res).then((data) => ({ res, data })))
      .then(({ res, data }) => {
        if (cancelled) return;
        if (!res.ok) throw new Error("Stats unavailable");
        setStats(data);
        setStatus("ready");
      })
      .catch(() => {
        if (!cancelled) setStatus("error");
      });
    return () => {
      cancelled = true;
    };
  }, [initialStats]);

  return { stats, status };
}

function useDebouncedValue<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timeout = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timeout);
  }, [delay, value]);
  return debounced;
}

function statGroups(stats: Stats) {
  const libraries = (stats.libraryCounts || []).map((count) => ({
    kind: "Library",
    label: count.libraryName || "Library",
    count: Number(count.count || 0)
  }));
  const media = (stats.mediaTypeCounts || []).map((count) => ({
    kind: "Media",
    label: mediaLabels[count.mediaType || ""] || count.mediaType || "Items",
    count: Number(count.count || 0)
  }));
  return [...libraries, ...media].filter((count) => count.count > 0);
}

function readCatalogState(initialLibraryId: string): CatalogBrowserState {
  const params = new URLSearchParams(window.location.search);
  return {
    libraryId: params.get("libraryId") || initialLibraryId,
    q: params.get("q") || "",
    type: params.get("type") || "",
    sort: params.get("sort") || "added_at",
    desc: params.get("desc") !== "false",
    genre: params.get("genre") || "",
    year: params.get("year") || "",
    decade: params.get("decade") || ""
  };
}

function writeCatalogState(state: CatalogBrowserState, token: string, replace: boolean) {
  const params = new URLSearchParams(window.location.search);
  setSearchParam(params, "libraryId", state.libraryId);
  setSearchParam(params, "q", state.q);
  setSearchParam(params, "type", state.type);
  setSearchParam(params, "sort", state.sort === "added_at" ? "" : state.sort);
  setSearchParam(params, "desc", state.desc ? "" : "false");
  setSearchParam(params, "genre", state.genre);
  setSearchParam(params, "year", state.year);
  setSearchParam(params, "decade", state.decade);
  if (token) params.set("token", token);
  const next = `${window.location.pathname}${params.toString() ? `?${params.toString()}` : ""}`;
  if (replace) {
    window.history.replaceState(null, "", next);
  } else {
    window.history.pushState(null, "", next);
  }
}

function canonicalCatalogState(state: CatalogBrowserState, libraries: Count[], initialLibraryId: string): CatalogBrowserState {
  const fallbackLibraryId = state.libraryId || initialLibraryId || String(libraries[0]?.libraryId || "");
  const library = libraries.find((item) => String(item.libraryId || "") === fallbackLibraryId) || libraries[0] || null;
  const libraryId = library ? String(library.libraryId || "") : "";
  const type = canonicalCatalogType(state.type, library?.mediaType || "");
  return { ...state, libraryId, type };
}

function canonicalCatalogType(rawType: string, rawLibraryType: string) {
  const options = catalogTypeOptions(normalizeLibraryMode(rawLibraryType));
  if (options.length === 0) return "";
  return options.some((option) => option.value === rawType) ? rawType : options[0].value;
}

function catalogTypeOptions(mode: LibraryMode) {
  switch (mode) {
    case "tv":
      return [
        { value: "tv", label: "Series" },
        { value: "episode", label: "Episodes" }
      ];
    case "mixed":
      return [
        { value: "movie", label: "Movies" },
        { value: "tv", label: "Series" }
      ];
    default:
      return [];
  }
}

function defaultTypeForLibrary(rawLibraryType: string) {
  return canonicalCatalogType("", rawLibraryType);
}

function normalizeLibraryMode(raw: string): LibraryMode {
  switch (raw.toLowerCase()) {
    case "movie":
    case "movies":
      return "movie";
    case "tv":
    case "series":
    case "shows":
      return "tv";
    case "mixed":
      return "mixed";
    default:
      return "unknown";
  }
}

function buildCatalogParams(state: CatalogBrowserState, token: string) {
  const params = new URLSearchParams();
  params.set("library_id", state.libraryId);
  params.set("sort", state.sort);
  params.set("desc", String(state.desc));
  params.set("page_size", "60");
  if (token) params.set("token", token);
  if (state.q.trim()) params.set("q", state.q.trim());
  if (state.type) params.set("media_type", state.type);
  if (state.genre) params.set("genre", state.genre);
  if (state.year) {
    params.set("year_min", state.year);
    params.set("year_max", state.year);
  } else if (state.decade) {
    const start = Number.parseInt(state.decade, 10);
    if (!Number.isNaN(start)) {
      params.set("year_min", String(start));
      params.set("year_max", String(start + 9));
    }
  }
  return params;
}

function catalogStateKey(state: CatalogBrowserState) {
  return JSON.stringify(state);
}

function catalogFilterKey(libraryId: string, type: string) {
  return JSON.stringify({ libraryId, type });
}

function normalizeCatalogResponse(data: CatalogResponse): CatalogResponse {
  return {
    items: data.items || data.Items || [],
    nextPageToken: data.nextPageToken || data.NextPageToken || "",
    totalCount: data.totalCount || data.TotalCount || 0
  };
}

function detailPills(detail: Detail) {
  const pills: string[] = [];
  if (detail.type === "episode" && detail.seasonNumber != null && detail.episodeNumber != null) {
    pills.push(`S${detail.seasonNumber} · E${detail.episodeNumber}`);
  }
  if (detail.type === "season" && detail.episodeCount) {
    pills.push(`${detail.episodeCount} episodes`);
  }
  if (detail.year) pills.push(String(detail.year));
  if (detail.contentRating) pills.push(detail.contentRating);
  if (detail.runtime) pills.push(formatRuntime(detail.runtime));
  if (detail.seasonCount) pills.push(`${detail.seasonCount} seasons`);
  if (detail.airDate) pills.push(detail.airDate);
  return pills;
}

function seasonLabel(seasonNumber: number, title: string, isSpecials: boolean) {
  if (title && !/^season\s+\d+$/i.test(title)) return title;
  if (isSpecials || seasonNumber === 0) return "Specials";
  return `Season ${seasonNumber}`;
}

async function readJSON<T>(res: Response): Promise<T> {
  const text = await res.text();
  if (!text) return {} as T;
  return JSON.parse(text) as T;
}

function errorMessage(data: unknown) {
  if (!data || typeof data !== "object") return "";
  const maybe = data as { error?: { message?: string }; message?: string };
  return maybe.error?.message || maybe.message || "";
}

function normalizedID(item: CatalogItem) {
  return item.mediaId || item.MediaID || normalizedTitle(item);
}

function normalizedTitle(item: CatalogItem) {
  return item.title || item.Title || "Untitled";
}

function normalizedSeriesTitle(item: CatalogItem) {
  return item.seriesTitle || item.SeriesTitle || "";
}

function normalizedMediaType(item: CatalogItem) {
  return item.mediaType || item.MediaType || item.type || item.Type || "";
}

function formatCount(value: number) {
  return Number(value || 0).toLocaleString();
}

function formatRuntime(minutes: number) {
  if (!minutes) return "";
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return hours ? `${hours}h ${mins}m` : `${mins}m`;
}

function libraryInitials(name: string) {
  const parts = name.split(/\s+/).filter(Boolean);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return `${parts[0][0] || ""}${parts[1][0] || ""}`.toUpperCase();
}

function qualityLabel(quality: { resolution?: string; videoCodec?: string; audioCodec?: string; container?: string; hdr?: boolean; count: number }) {
  return [quality.resolution, quality.hdr ? "HDR" : "", quality.videoCodec, quality.audioCodec, quality.count > 1 ? `${quality.count} files` : ""].filter(Boolean).join(" · ");
}

function apiPath(path: string) {
  return path.startsWith("/") ? path : `/${path}`;
}

function publicRoute(path: string) {
  const trimmed = path.trim();
  if (!trimmed || trimmed === "/") {
    const base = publicRouteBase();
    return base || "/";
  }
  if (/^[a-z]+:\/\//i.test(trimmed)) return trimmed;
  if (trimmed.startsWith("#")) return trimmed;
  const base = publicRouteBase();
  const next = trimmed.replace(/^\/+/, "");
  return base ? `${base}/${next}` : `/${next}`;
}

function tokenQuery(token: string) {
  return token ? `?token=${encodeURIComponent(token)}` : "";
}

function withToken(path: string, token: string) {
  const [pathWithoutHash, hash = ""] = path.split("#", 2);
  const [base, query = ""] = pathWithoutHash.split("?", 2);
  const params = new URLSearchParams(query);
  if (token) params.set("token", token);
  const theme = currentTheme();
  if (theme) params.set("theme", theme);
  const nextQuery = params.toString();
  return `${base}${nextQuery ? `?${nextQuery}` : ""}${hash ? `#${hash}` : ""}`;
}

function buildItemHref(id: string, token: string, libraryId: string) {
  const path = libraryId ? publicRoute(`item/${encodeURIComponent(id)}?libraryId=${encodeURIComponent(libraryId)}`) : publicRoute(`item/${encodeURIComponent(id)}`);
  return withToken(path, token);
}

function sameCatalogState(left: CatalogBrowserState, right: CatalogBrowserState) {
  return JSON.stringify(left) === JSON.stringify(right);
}

function setSearchParam(params: URLSearchParams, key: string, value: string) {
  if (value) {
    params.set(key, value);
  } else {
    params.delete(key);
  }
}

const sampleCustomHTML =
  '<div class="custom-html-sample"><h3>Featured note</h3><p>This area is the custom HTML section for a named public page. It can hold event copy, access notes, or curated instructions while the rest of the page keeps the Continuum library experience.</p></div>';

function normalizeTheme(theme?: string | null) {
  if (!theme) return "";
  const next = theme.trim();
  return supportedThemes.has(next) ? next : "";
}

function currentTheme() {
  return normalizeTheme(document.documentElement.dataset.theme) || resolveThemePreference(bootstrap.theme);
}

function resolveThemePreference(bootstrapTheme: string) {
  const requested = normalizeTheme(new URLSearchParams(window.location.search).get("theme"));
  if (requested) return requested;
  try {
    const saved = normalizeTheme(window.localStorage.getItem(PUBLIC_THEME_STORAGE_KEY));
    if (saved) return saved;
  } catch {}
  return normalizeTheme(bootstrapTheme) || DARK_THEME;
}

function publicRouteBase() {
  const match = window.location.pathname.match(/^(\/api\/v1\/plugins\/\d+)(?:\/.*)?$/);
  return match ? match[1] : "";
}

createRoot(document.getElementById("root")!).render(<App />);
