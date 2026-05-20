import { useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { AuthPrompt } from "@/components/catalog/AuthPrompt";
import { CatalogToolbar, type CatalogToolbarValue } from "@/components/catalog/CatalogToolbar";
import { MediaCard } from "@/components/shared/MediaCard";
import { TopBar } from "@/components/shared/TopBar";
import { getCatalogFilters, getCatalogMedia, type CatalogMediaQuery } from "@/api/catalog";
import { formatCount } from "@/lib/format";
import type { CatalogFilters, CatalogMediaItem, PublicBootstrap } from "@/lib/types";

type Props = { bootstrap: PublicBootstrap };

const DEBOUNCE_MS = 250;

// CatalogPage hosts the searchable catalog browser. It hydrates from
// the bootstrap (initial items, filters, library) so the first render
// already shows content, then fetches fresh data as the user changes
// the toolbar. nextPageToken-driven "load more" supports the larger
// libraries without pagination UI.
export function CatalogPage({ bootstrap }: Props) {
  const [authorized, setAuthorized] = useState(!bootstrap.authRequired);
  if (!authorized) {
    return <AuthPrompt onAuthorized={() => setAuthorized(true)} />;
  }
  return <CatalogBrowser bootstrap={bootstrap} />;
}

function CatalogBrowser({ bootstrap }: { bootstrap: PublicBootstrap }) {
  const libraries = useMemo(() => bootstrap.initialStats?.libraryCounts ?? [], [bootstrap]);
  const [toolbar, setToolbar] = useState<CatalogToolbarValue>({
    query: "",
    mediaType: "all",
    libraryId: bootstrap.initialLibraryId ?? "",
    genre: "",
    yearBucket: "",
    sort: "added_at",
    desc: true,
  });
  const [items, setItems] = useState<CatalogMediaItem[]>(bootstrap.initialItems ?? []);
  const [nextPageToken, setNextPageToken] = useState<string | undefined>(bootstrap.initialNextPageToken);
  const [totalCount, setTotalCount] = useState<number>(bootstrap.initialTotalCount ?? 0);
  const [filters, setFilters] = useState<CatalogFilters | undefined>(bootstrap.initialFilters);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const debounceTimer = useRef<number | null>(null);

  const queryArgs = useMemo<CatalogMediaQuery>(() => {
    const yearMin = toolbar.yearBucket ? Number(toolbar.yearBucket.split("-")[0]) : undefined;
    const yearMax = toolbar.yearBucket ? Number(toolbar.yearBucket.split("-")[1]) : undefined;
    return {
      q: toolbar.query.trim(),
      genre: toolbar.genre || undefined,
      yearMin,
      yearMax,
      sort: toolbar.sort,
      desc: toolbar.desc,
      pageSize: 48,
      libraryIds: toolbar.libraryId ? [toolbar.libraryId] : undefined,
      mediaTypes: toolbar.mediaType === "all" ? undefined : [toolbar.mediaType],
      token: bootstrap.token || undefined,
    };
  }, [toolbar, bootstrap.token]);

  // Debounce the catalog fetch so search keystrokes don't fire N
  // requests; AbortController cancels in-flight queries that the user
  // has already moved past.
  useEffect(() => {
    if (debounceTimer.current) window.clearTimeout(debounceTimer.current);
    const controller = new AbortController();
    debounceTimer.current = window.setTimeout(() => {
      setLoading(true);
      setError("");
      getCatalogMedia({ ...queryArgs, signal: controller.signal })
        .then((resp) => {
          setItems(resp.items ?? []);
          setNextPageToken(resp.nextPageToken);
          setTotalCount(resp.totalCount ?? 0);
        })
        .catch((err) => {
          if (err instanceof DOMException && err.name === "AbortError") return;
          setError(err instanceof Error ? err.message : "Catalog request failed");
        })
        .finally(() => setLoading(false));
    }, DEBOUNCE_MS);
    return () => {
      controller.abort();
      if (debounceTimer.current) window.clearTimeout(debounceTimer.current);
    };
  }, [queryArgs]);

  // Filters are independent of pagination — refresh when the
  // library/type scope changes.
  useEffect(() => {
    let cancelled = false;
    getCatalogFilters({
      libraryIds: queryArgs.libraryIds,
      mediaTypes: queryArgs.mediaTypes,
      token: queryArgs.token,
    })
      .then((next) => {
        if (!cancelled) setFilters(next);
      })
      .catch(() => {
        // best-effort — keep whatever filters we already had
      });
    return () => {
      cancelled = true;
    };
  }, [queryArgs.libraryIds, queryArgs.mediaTypes, queryArgs.token]);

  async function loadMore() {
    if (!nextPageToken || loadingMore) return;
    setLoadingMore(true);
    try {
      const resp = await getCatalogMedia({ ...queryArgs, pageToken: nextPageToken });
      setItems((current) => [...current, ...(resp.items ?? [])]);
      setNextPageToken(resp.nextPageToken);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Load more failed");
    } finally {
      setLoadingMore(false);
    }
  }

  return (
    <main className="min-h-[100dvh] bg-background text-foreground">
      <div className="mx-auto max-w-7xl space-y-6 px-4 py-8 md:px-8">
        <TopBar
          eyebrow="Browse"
          title="Catalog"
          subtitle="Search the public catalog and confirm what's available before requesting access."
          trailing={
            <p className="text-sm text-muted-foreground">
              <strong className="text-foreground">{formatCount(totalCount)}</strong> items
            </p>
          }
        />

        <CatalogToolbar
          value={toolbar}
          onChange={setToolbar}
          filters={filters}
          libraries={libraries}
        />

        {error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {loading && items.length === 0 ? (
          <PosterSkeletons />
        ) : items.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-10 text-center text-muted-foreground">
            <p className="font-medium text-foreground">No matches.</p>
            <p>Try a broader search, a different library, or clear the filters.</p>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7">
              {items.map((item) => (
                <MediaCard
                  key={`${item.mediaType}:${item.mediaId}`}
                  item={item}
                  token={bootstrap.token}
                  libraryId={toolbar.libraryId}
                />
              ))}
            </div>
            {nextPageToken && (
              <div className="flex justify-center pt-4">
                <Button variant="secondary" disabled={loadingMore} onClick={loadMore}>
                  {loadingMore ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </main>
  );
}

function PosterSkeletons() {
  return (
    <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7">
      {Array.from({ length: 14 }).map((_, i) => (
        <div key={i} className="space-y-2">
          <Skeleton className="aspect-[2/3] w-full rounded-md" />
          <Skeleton className="h-3 w-3/4 rounded" />
          <Skeleton className="h-2 w-1/2 rounded" />
        </div>
      ))}
    </div>
  );
}
