import { api } from "@/lib/api";
import type {
  CatalogEpisode,
  CatalogFilters,
  CatalogItemDetail,
  CatalogMediaResponse,
  CatalogSeason,
} from "@/lib/types";

export type CatalogMediaQuery = {
  q?: string;
  genre?: string;
  yearMin?: number;
  yearMax?: number;
  sort?: string;
  desc?: boolean;
  pageSize?: number;
  pageToken?: string;
  mediaTypes?: string[];
  libraryIds?: string[];
  token?: string;
  signal?: AbortSignal;
};

function buildCatalogParams(q: CatalogMediaQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (q.q) params.set("q", q.q);
  if (q.genre) params.set("genre", q.genre);
  if (q.yearMin) params.set("year_min", String(q.yearMin));
  if (q.yearMax) params.set("year_max", String(q.yearMax));
  if (q.sort) params.set("sort", q.sort);
  if (q.desc) params.set("desc", "true");
  if (q.pageSize) params.set("page_size", String(q.pageSize));
  if (q.pageToken) params.set("page_token", q.pageToken);
  if (q.token) params.set("token", q.token);
  for (const mt of q.mediaTypes ?? []) {
    params.append("media_type", mt);
  }
  for (const lib of q.libraryIds ?? []) {
    params.append("library_id", lib);
  }
  return params;
}

export function getCatalogMedia(q: CatalogMediaQuery): Promise<CatalogMediaResponse> {
  return api<CatalogMediaResponse>(`/api/catalog/media?${buildCatalogParams(q).toString()}`, {
    signal: q.signal,
  });
}

export function getCatalogFilters(q: Pick<CatalogMediaQuery, "mediaTypes" | "libraryIds" | "token">): Promise<CatalogFilters> {
  return api<CatalogFilters>(`/api/catalog/filters?${buildCatalogParams(q).toString()}`);
}

export function getCatalogItem(id: string, token?: string): Promise<CatalogItemDetail> {
  const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
  return api<CatalogItemDetail>(`/api/catalog/items/${encodeURIComponent(id)}${suffix}`);
}

export function getCatalogSeasons(seriesId: string, token?: string): Promise<CatalogSeason[]> {
  const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
  return api<CatalogSeason[]>(`/api/catalog/items/${encodeURIComponent(seriesId)}/seasons${suffix}`);
}

export function getCatalogEpisodes(seriesId: string, seasonNumber: number, token?: string): Promise<CatalogEpisode[]> {
  const suffix = token ? `?token=${encodeURIComponent(token)}` : "";
  return api<CatalogEpisode[]>(`/api/catalog/series/${encodeURIComponent(seriesId)}/seasons/${seasonNumber}/episodes${suffix}`);
}

export function catalogLogin(password: string): Promise<{ ok: boolean }> {
  return api<{ ok: boolean }>("/api/public/catalog-login", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
}
