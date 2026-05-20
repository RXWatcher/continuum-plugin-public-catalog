package store

import "time"

// CatalogStats is the aggregate count payload for the landing page and
// admin overview. Counts come from a single read of public.media_items
// (and a small extra read for "4K" quality buckets).
type CatalogStats struct {
	TotalItems      int                   `json:"totalItems"`
	MediaTypeCounts []CatalogTypeCount    `json:"mediaTypeCounts"`
	LibraryCounts   []CatalogLibraryCount `json:"libraryCounts"`
	QualityCounts   []CatalogQualityCount `json:"qualityCounts"`
}

type CatalogTypeCount struct {
	MediaType string `json:"mediaType"`
	Count     int    `json:"count"`
}

type CatalogLibraryCount struct {
	LibraryID   string `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	MediaType   string `json:"mediaType"`
	Count       int    `json:"count"`
}

type CatalogQualityCount struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// CatalogFilters is the filter dictionary the catalog UI uses to render
// genre / year / decade dropdowns.
type CatalogFilters struct {
	Genres  []string        `json:"genres"`
	Years   []int           `json:"years"`
	Decades []CatalogDecade `json:"decades"`
}

type CatalogDecade struct {
	Label   string `json:"label"`
	YearMin int    `json:"yearMin"`
	YearMax int    `json:"yearMax"`
}

// CatalogMediaQuery is the validated input to CatalogMedia.
type CatalogMediaQuery struct {
	LibraryIDs []string
	MediaTypes []string
	Query      string
	Genre      string
	YearMin    int
	YearMax    int
	Sort       string
	Descending bool
	PageSize   int
	PageToken  string
}

// CatalogMediaResponse is one page of catalog browsing.
type CatalogMediaResponse struct {
	Items         []CatalogMediaItem `json:"items"`
	NextPageToken string             `json:"nextPageToken"`
	TotalCount    int                `json:"totalCount"`
}

// CatalogMediaItem is the projection returned to the catalog UI.
type CatalogMediaItem struct {
	MediaID        string   `json:"mediaId"`
	LibraryID      string   `json:"libraryId,omitempty"`
	MediaType      string   `json:"mediaType"`
	Title          string   `json:"title"`
	SeriesTitle    string   `json:"seriesTitle,omitempty"`
	SeasonNumber   int      `json:"seasonNumber,omitempty"`
	EpisodeNumber  int      `json:"episodeNumber,omitempty"`
	Year           int      `json:"year,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	PosterURL      string   `json:"posterUrl,omitempty"`
	BackdropURL    string   `json:"backdropUrl,omitempty"`
	Genres         []string `json:"genres"`
	RuntimeMinutes int      `json:"runtimeMinutes,omitempty"`
	Rating         float64  `json:"rating,omitempty"`
	ContentRating  string   `json:"contentRating,omitempty"`
	AddedAt        string   `json:"addedAt,omitempty"`
	tmdbID         string
	tvdbID         string
}

// CatalogMediaImagePaths holds the host-provided poster/backdrop paths
// for a single media id (used to enrich items with provider images).
type CatalogMediaImagePaths struct {
	MediaID      string
	PosterPath   string
	BackdropPath string
}

// CatalogItemDetail is the rich detail view for one item (movie, show,
// season, or episode).
type CatalogItemDetail struct {
	ContentID        string           `json:"contentId"`
	Type             string           `json:"type"`
	Title            string           `json:"title"`
	OriginalTitle    string           `json:"originalTitle,omitempty"`
	SeriesID         string           `json:"seriesId,omitempty"`
	SeriesTitle      string           `json:"seriesTitle,omitempty"`
	SeasonNumber     int              `json:"seasonNumber,omitempty"`
	EpisodeNumber    int              `json:"episodeNumber,omitempty"`
	EpisodeCount     int              `json:"episodeCount,omitempty"`
	Year             int              `json:"year,omitempty"`
	Overview         string           `json:"overview,omitempty"`
	Tagline          string           `json:"tagline,omitempty"`
	Runtime          int              `json:"runtime,omitempty"`
	ContentRating    string           `json:"contentRating,omitempty"`
	Genres           []string         `json:"genres"`
	Studios          []string         `json:"studios,omitempty"`
	Networks         []string         `json:"networks,omitempty"`
	Countries        []string         `json:"countries,omitempty"`
	FirstAirDate     string           `json:"firstAirDate,omitempty"`
	LastAirDate      string           `json:"lastAirDate,omitempty"`
	ReleaseDate      string           `json:"releaseDate,omitempty"`
	RatingIMDB       float64          `json:"ratingImdb,omitempty"`
	RatingTMDB       float64          `json:"ratingTmdb,omitempty"`
	RatingRTCritic   int              `json:"ratingRtCritic,omitempty"`
	RatingRTAudience int              `json:"ratingRtAudience,omitempty"`
	PosterURL        string           `json:"posterUrl,omitempty"`
	BackdropURL      string           `json:"backdropUrl,omitempty"`
	LogoURL          string           `json:"logoUrl,omitempty"`
	SeasonCount      int              `json:"seasonCount,omitempty"`
	AirDate          string           `json:"airDate,omitempty"`
	IsSpecials       bool             `json:"isSpecials,omitempty"`
	Libraries        []CatalogLibrary `json:"libraries,omitempty"`
	Qualities        []CatalogQuality `json:"qualities,omitempty"`
	tmdbID           string
	tvdbID           string
}

type CatalogLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type CatalogQuality struct {
	Resolution string `json:"resolution,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	Container  string `json:"container,omitempty"`
	HDR        bool   `json:"hdr,omitempty"`
	Count      int    `json:"count"`
}

type CatalogSeason struct {
	ContentID    string `json:"contentId"`
	SeriesID     string `json:"seriesId"`
	SeasonNumber int    `json:"seasonNumber"`
	Title        string `json:"title"`
	Overview     string `json:"overview,omitempty"`
	AirDate      string `json:"airDate,omitempty"`
	PosterURL    string `json:"posterUrl,omitempty"`
	EpisodeCount int    `json:"episodeCount"`
	tmdbID       string
	tvdbID       string
}

type CatalogEpisode struct {
	ContentID     string  `json:"contentId"`
	SeriesID      string  `json:"seriesId"`
	SeasonID      string  `json:"seasonId,omitempty"`
	SeasonNumber  int     `json:"seasonNumber"`
	EpisodeNumber int     `json:"episodeNumber"`
	Title         string  `json:"title"`
	Overview      string  `json:"overview,omitempty"`
	AirDate       string  `json:"airDate,omitempty"`
	Runtime       int     `json:"runtime,omitempty"`
	RatingIMDB    float64 `json:"ratingImdb,omitempty"`
	RatingTMDB    float64 `json:"ratingTmdb,omitempty"`
	StillURL      string  `json:"stillUrl,omitempty"`
}

// SavedCatalogLink is one operator-issued bypass link with its scope.
type SavedCatalogLink struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Token      string    `json:"token"`
	URL        string    `json:"url"`
	HTML       string    `json:"html"`
	MediaTypes []string  `json:"mediaTypes"`
	LibraryIDs []string  `json:"libraryIds"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
