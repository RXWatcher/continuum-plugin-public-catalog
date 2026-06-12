// Wire shapes. Field names match the backend JSON exactly (camelCase
// from the Go store types).

export type CatalogStats = {
  totalItems: number;
  mediaTypeCounts?: CatalogTypeCount[];
  libraryCounts?: CatalogLibraryCount[];
  qualityCounts?: CatalogQualityCount[];
};

export type CatalogTypeCount = { mediaType: string; count: number };
export type CatalogLibraryCount = {
  libraryId: string;
  libraryName: string;
  mediaType: string;
  count: number;
};
export type CatalogQualityCount = { key: string; label: string; count: number };

export type CatalogFilters = {
  genres?: string[];
  years?: number[];
  decades?: CatalogDecade[];
};
export type CatalogDecade = { label: string; yearMin: number; yearMax: number };

export type CatalogMediaItem = {
  mediaId: string;
  libraryId?: string;
  mediaType: string;
  title: string;
  seriesTitle?: string;
  seasonNumber?: number;
  episodeNumber?: number;
  year?: number;
  overview?: string;
  posterUrl?: string;
  backdropUrl?: string;
  genres?: string[];
  runtimeMinutes?: number;
  rating?: number;
  contentRating?: string;
  addedAt?: string;
};

export type CatalogMediaResponse = {
  items: CatalogMediaItem[];
  nextPageToken?: string;
  totalCount?: number;
};

export type CatalogItemDetail = {
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
  ratingImdb?: number;
  ratingTmdb?: number;
  ratingRtCritic?: number;
  ratingRtAudience?: number;
  posterUrl?: string;
  backdropUrl?: string;
  logoUrl?: string;
  seasonCount?: number;
  airDate?: string;
  isSpecials?: boolean;
  libraries?: CatalogLibrary[];
  qualities?: CatalogQuality[];
};

export type CatalogLibrary = { id: string; name: string; type: string };
export type CatalogQuality = {
  resolution?: string;
  videoCodec?: string;
  audioCodec?: string;
  container?: string;
  hdr?: boolean;
  count: number;
};

export type CatalogSeason = {
  contentId: string;
  seriesId: string;
  seasonNumber: number;
  title: string;
  overview?: string;
  airDate?: string;
  posterUrl?: string;
  episodeCount: number;
};

export type CatalogEpisode = {
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

export type SavedCatalogLink = {
  id: number;
  name: string;
  // slug is the unguessable identifier used in the public landing URL
  // (?page=<slug>). The display name is never accepted for resolution.
  slug: string;
  token: string;
  url: string;
  html: string;
  mediaTypes?: string[];
  libraryIds?: string[];
  createdAt: string;
  updatedAt: string;
};

// PluginConfig mirrors runtime.Config — the API returns it with
// secret fields blanked out (token_secret, catalog_password, and the
// bcrypt hash). The boolean catalog_password_set signals whether a
// password is configured without ever exposing the hash.
export type PluginConfig = {
  token_secret?: string;
  token_secret_generated?: boolean;
  standalone_http_listen?: string;
  public_port?: number;
  public_base_url?: string;
  ad_html?: string;
  published_html?: string;
  catalog_password?: string;
  // catalog_password_set is the response-only boolean signalling whether
  // a password hash is stored. The bcrypt hash itself is never sent.
  catalog_password_set?: boolean;
  catalog_password_required: boolean;
  token_ttl_hours?: number;
  ebook_installation_id?: string;
  audio_installation_id?: string;
  // public_library_ids is the hard allowlist of host library ids the
  // public catalog may ever expose. Empty means expose nothing.
  public_library_ids?: string[];
};

// PublicBootstrap is the JSON the server bakes into the SPA shell
// (window.__PUBLIC_CATALOG_BOOTSTRAP). Shape mirrors
// server.publicBootstrap.
export type PublicBootstrap = {
  mode: "landing" | "catalog" | "detail" | "admin";
  theme: string;
  catalogHref: string;
  customHTML: string;
  authRequired: boolean;
  token: string;
  initialStats?: CatalogStats;
  initialLibraryId?: string;
  initialItems?: CatalogMediaItem[];
  initialNextPageToken?: string;
  initialTotalCount?: number;
  initialFilters?: CatalogFilters;
};

export type APIError = Error & {
  responseStatus?: number;
  responseCode?: string;
};
