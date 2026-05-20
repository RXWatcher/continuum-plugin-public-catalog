import { useState } from "react";
import { Film } from "lucide-react";

import { titleCase } from "@/lib/format";
import type { CatalogMediaItem } from "@/lib/types";

type Props = {
  item: CatalogMediaItem;
  token?: string;
  libraryId?: string;
};

// MediaCard is the catalog grid tile: poster (with fallback), title,
// season/episode label for TV, year+type meta line. Clicking opens the
// item detail page with the same token / scope preserved on the URL.
export function MediaCard({ item, token, libraryId }: Props) {
  const [posterFailed, setPosterFailed] = useState(false);
  const href = buildDetailHref(item, token, libraryId);
  const meta = buildMetaLine(item);
  const subtitle = buildSubtitleLine(item);
  const initials = (item.title || "?").slice(0, 1).toUpperCase();

  return (
    <a
      href={href}
      className="group flex flex-col gap-2 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
    >
      <div className="relative aspect-[2/3] overflow-hidden rounded-md border border-border bg-card shadow-sm transition-[transform,border-color] group-hover:-translate-y-1 group-hover:border-accent/50">
        {item.posterUrl && !posterFailed ? (
          <img
            src={item.posterUrl}
            alt=""
            loading="lazy"
            onError={() => setPosterFailed(true)}
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full flex-col items-center justify-center gap-2 px-3 text-center text-muted-foreground">
            <Film className="h-6 w-6 opacity-60" />
            <span className="line-clamp-3 text-xs font-medium">{item.title || initials}</span>
          </div>
        )}
        <div className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-black/60 to-transparent" />
      </div>
      <div className="space-y-1">
        <h3 className="truncate text-sm font-semibold leading-tight">{item.title}</h3>
        {subtitle && <p className="truncate text-xs text-muted-foreground">{subtitle}</p>}
        {meta && (
          <p className="truncate text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {meta}
          </p>
        )}
      </div>
    </a>
  );
}

function buildDetailHref(item: CatalogMediaItem, token?: string, libraryId?: string): string {
  const params = new URLSearchParams();
  if (token) params.set("token", token);
  if (libraryId) params.set("libraryId", libraryId);
  const suffix = params.toString() ? `?${params.toString()}` : "";
  return `../item/${encodeURIComponent(item.mediaId)}${suffix}`;
}

function buildSubtitleLine(item: CatalogMediaItem): string {
  if (item.mediaType === "episode" && item.seriesTitle) {
    const num = item.seasonNumber && item.episodeNumber
      ? `S${item.seasonNumber}E${item.episodeNumber} · `
      : "";
    return `${num}${item.seriesTitle}`;
  }
  return item.overview ?? "";
}

function buildMetaLine(item: CatalogMediaItem): string {
  const parts: string[] = [];
  if (item.year) parts.push(String(item.year));
  if (item.mediaType) parts.push(titleCase(item.mediaType));
  if (item.contentRating) parts.push(item.contentRating);
  return parts.join(" · ");
}
