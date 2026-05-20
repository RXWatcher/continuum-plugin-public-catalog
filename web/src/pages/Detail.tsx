import { useEffect, useState } from "react";
import { ArrowLeft, Film, Star } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getCatalogEpisodes, getCatalogItem, getCatalogSeasons } from "@/api/catalog";
import { formatRuntimeMinutes, formatShortDate } from "@/lib/format";
import type { CatalogEpisode, CatalogItemDetail, CatalogSeason, PublicBootstrap } from "@/lib/types";

type Props = { bootstrap: PublicBootstrap };

// DetailPage shows a single catalog item: movie, series, season, or
// episode. The contentId comes from the URL path /item/{id}.
export function DetailPage({ bootstrap }: Props) {
  const contentId = decodeURIComponent(window.location.pathname.split("/item/")[1] ?? "");
  const [detail, setDetail] = useState<CatalogItemDetail | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    setError("");
    getCatalogItem(contentId, bootstrap.token || undefined)
      .then((next) => {
        if (!cancelled) setDetail(next);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Item not found");
      });
    return () => {
      cancelled = true;
    };
  }, [contentId, bootstrap.token]);

  if (error) {
    return (
      <main className="mx-auto max-w-3xl px-4 py-16 text-center">
        <p className="mb-2 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Catalog
        </p>
        <h1 className="mb-4 text-2xl font-semibold">Item unavailable</h1>
        <p className="text-muted-foreground">{error}</p>
        <BackToCatalog token={bootstrap.token} />
      </main>
    );
  }

  if (!detail) return <DetailSkeleton />;

  return (
    <main className="min-h-[100dvh] bg-background text-foreground">
      {detail.backdropUrl && (
        <div className="relative isolate h-[280px] w-full overflow-hidden md:h-[420px]">
          <img src={detail.backdropUrl} alt="" className="h-full w-full object-cover" />
          <div className="absolute inset-0 bg-gradient-to-t from-background via-background/60 to-background/0" />
        </div>
      )}
      <div className="mx-auto -mt-32 max-w-6xl space-y-8 px-4 pb-16 md:px-8">
        <BackToCatalog token={bootstrap.token} />
        <DetailHero detail={detail} />
        <DetailBody detail={detail} token={bootstrap.token || undefined} />
      </div>
    </main>
  );
}

function BackToCatalog({ token }: { token: string }) {
  const href = token ? `../catalog?token=${encodeURIComponent(token)}` : "../catalog";
  return (
    <a
      href={href}
      className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm"
    >
      <ArrowLeft className="h-4 w-4" />
      Back to catalog
    </a>
  );
}

function DetailHero({ detail }: { detail: CatalogItemDetail }) {
  return (
    <section className="flex flex-col gap-6 md:flex-row md:items-end">
      <div className="w-40 shrink-0 overflow-hidden rounded-md border border-border bg-card shadow-lg md:w-56">
        {detail.posterUrl ? (
          <img src={detail.posterUrl} alt="" className="aspect-[2/3] w-full object-cover" />
        ) : (
          <div className="flex aspect-[2/3] w-full items-center justify-center text-muted-foreground">
            <Film className="h-10 w-10 opacity-50" />
          </div>
        )}
      </div>
      <div className="flex-1 space-y-3">
        <p className="text-xs font-semibold uppercase tracking-[0.16em] text-accent">
          {detail.type === "season" ? "Season" : detail.type === "episode" ? "Episode" : detail.type}
        </p>
        <h1 className="text-3xl font-semibold leading-tight md:text-5xl">{detail.title}</h1>
        {detail.originalTitle && detail.originalTitle !== detail.title && (
          <p className="text-sm text-muted-foreground">{detail.originalTitle}</p>
        )}
        {detail.tagline && <p className="text-base italic text-muted-foreground">{detail.tagline}</p>}
        <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          {detail.year ? <Badge variant="secondary">{detail.year}</Badge> : null}
          {detail.runtime ? <Badge variant="secondary">{formatRuntimeMinutes(detail.runtime)}</Badge> : null}
          {detail.contentRating ? <Badge variant="secondary">{detail.contentRating}</Badge> : null}
          {(detail.ratingTmdb ?? 0) > 0 && (
            <span className="inline-flex items-center gap-1">
              <Star className="h-3.5 w-3.5 fill-current text-amber-400" />
              {detail.ratingTmdb!.toFixed(1)}
            </span>
          )}
        </div>
        {(detail.genres ?? []).length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {detail.genres!.map((g) => (
              <Badge key={g} variant="outline">
                {g}
              </Badge>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}

function DetailBody({ detail, token }: { detail: CatalogItemDetail; token?: string }) {
  return (
    <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
      <div className="space-y-6">
        {detail.overview && (
          <Card>
            <CardContent className="prose prose-invert max-w-none py-5 text-sm leading-relaxed text-muted-foreground">
              {detail.overview}
            </CardContent>
          </Card>
        )}
        {detail.type === "series" && <SeriesSeasons seriesId={detail.contentId} token={token} />}
        {detail.type === "season" && detail.seriesId && (
          <SeasonEpisodes
            seriesId={detail.seriesId}
            seasonNumber={detail.seasonNumber ?? 0}
            token={token}
          />
        )}
      </div>
      <div className="space-y-4">
        <MetadataPanel detail={detail} />
        <AvailabilityPanel detail={detail} />
      </div>
    </div>
  );
}

function MetadataPanel({ detail }: { detail: CatalogItemDetail }) {
  const rows: Array<[string, string | undefined]> = [
    ["Released", formatShortDate(detail.releaseDate || detail.firstAirDate)],
    ["Last episode", formatShortDate(detail.lastAirDate)],
    ["Studios", detail.studios?.join(", ")],
    ["Networks", detail.networks?.join(", ")],
    ["Countries", detail.countries?.join(", ")],
    ["Seasons", detail.seasonCount ? String(detail.seasonCount) : undefined],
  ].filter(([, v]) => Boolean(v)) as Array<[string, string]>;
  if (!rows.length) return null;
  return (
    <Card>
      <CardContent className="space-y-3 py-5">
        <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Details
        </h2>
        <dl className="grid grid-cols-1 gap-y-2 text-sm">
          {rows.map(([label, value]) => (
            <div key={label} className="flex flex-col">
              <dt className="text-xs text-muted-foreground">{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}

function AvailabilityPanel({ detail }: { detail: CatalogItemDetail }) {
  if (!(detail.libraries?.length || detail.qualities?.length)) return null;
  return (
    <Card>
      <CardContent className="space-y-4 py-5">
        <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Availability
        </h2>
        {(detail.libraries ?? []).length > 0 && (
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Libraries</p>
            <div className="flex flex-wrap gap-1.5">
              {detail.libraries!.map((l) => (
                <Badge key={l.id} variant="secondary">
                  {l.name}
                </Badge>
              ))}
            </div>
          </div>
        )}
        {(detail.qualities ?? []).length > 0 && (
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">Qualities</p>
            <ul className="space-y-1 text-sm">
              {detail.qualities!.map((q, i) => (
                <li key={i} className="flex items-center justify-between gap-2">
                  <span>
                    {[q.resolution, q.hdr ? "HDR" : null, q.videoCodec, q.audioCodec, q.container]
                      .filter(Boolean)
                      .join(" · ")}
                  </span>
                  <span className="text-xs text-muted-foreground">×{q.count}</span>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function SeriesSeasons({ seriesId, token }: { seriesId: string; token?: string }) {
  const [seasons, setSeasons] = useState<CatalogSeason[]>([]);
  useEffect(() => {
    let cancelled = false;
    getCatalogSeasons(seriesId, token)
      .then((next) => {
        if (!cancelled) setSeasons(next);
      })
      .catch(() => {
        // best-effort
      });
    return () => {
      cancelled = true;
    };
  }, [seriesId, token]);

  if (seasons.length === 0) return null;
  return (
    <Card>
      <CardContent className="space-y-3 py-5">
        <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Seasons
        </h2>
        <ul className="space-y-2">
          {seasons.map((season) => {
            const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
            return (
              <li key={season.contentId}>
                <a
                  href={`./${encodeURIComponent(season.contentId)}${suffix}`}
                  className="flex items-center justify-between gap-3 rounded-md border border-border bg-card px-3 py-2 transition-colors hover:border-accent/40"
                >
                  <span className="font-medium">{season.title}</span>
                  <span className="text-xs text-muted-foreground">{season.episodeCount} episodes</span>
                </a>
              </li>
            );
          })}
        </ul>
      </CardContent>
    </Card>
  );
}

function SeasonEpisodes({
  seriesId,
  seasonNumber,
  token,
}: {
  seriesId: string;
  seasonNumber: number;
  token?: string;
}) {
  const [episodes, setEpisodes] = useState<CatalogEpisode[]>([]);
  useEffect(() => {
    let cancelled = false;
    getCatalogEpisodes(seriesId, seasonNumber, token)
      .then((next) => {
        if (!cancelled) setEpisodes(next);
      })
      .catch(() => {
        // best-effort
      });
    return () => {
      cancelled = true;
    };
  }, [seriesId, seasonNumber, token]);

  if (episodes.length === 0) return null;
  return (
    <Card>
      <CardContent className="space-y-3 py-5">
        <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          Episodes
        </h2>
        <ul className="space-y-2">
          {episodes.map((ep) => (
            <li
              key={ep.contentId}
              className="rounded-md border border-border bg-card px-3 py-2"
            >
              <p className="text-sm font-medium">
                S{ep.seasonNumber}E{ep.episodeNumber} · {ep.title}
              </p>
              {ep.overview && (
                <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{ep.overview}</p>
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function DetailSkeleton() {
  return (
    <main className="mx-auto max-w-6xl space-y-6 px-4 py-10 md:px-8">
      <Skeleton className="h-72 w-full" />
      <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    </main>
  );
}
