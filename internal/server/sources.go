package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtimehost"
)

type CatalogSource interface {
	MediaType() string
	Stats(ctx context.Context, host Host) (*runtimehost.CatalogStats, error)
	List(ctx context.Context, host Host, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error)
}

type RuntimeHostSource struct {
	mediaType      string
	installationID int
}

func NewRuntimeHostSource(mediaType string, installationID int) *RuntimeHostSource {
	return &RuntimeHostSource{mediaType: mediaType, installationID: installationID}
}

func (s *RuntimeHostSource) MediaType() string { return s.mediaType }

func (s *RuntimeHostSource) Stats(ctx context.Context, host Host) (*runtimehost.CatalogStats, error) {
	if s == nil || s.installationID <= 0 {
		return &runtimehost.CatalogStats{}, nil
	}
	var out runtimehost.CatalogStats
	if err := s.getJSON(ctx, host, "/api/v1/public-catalog/stats", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *RuntimeHostSource) List(ctx context.Context, host Host, req runtimehost.ListLibraryMediaRequest) (*runtimehost.ListLibraryMediaResponse, error) {
	if s == nil || s.installationID <= 0 {
		return &runtimehost.ListLibraryMediaResponse{}, nil
	}
	query := map[string]any{}
	if req.Query != "" {
		query["q"] = req.Query
	}
	if req.Genre != "" {
		query["genre"] = req.Genre
	}
	if req.Sort != "" {
		query["sort"] = req.Sort
	}
	if req.Descending {
		query["desc"] = "true"
	}
	if req.PageSize > 0 {
		query["page_size"] = fmt.Sprintf("%d", req.PageSize)
	}
	if req.PageToken != "" {
		query["page_token"] = req.PageToken
	}
	var out runtimehost.ListLibraryMediaResponse
	if err := s.getJSON(ctx, host, "/api/v1/public-catalog/media", query, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *RuntimeHostSource) getJSON(ctx context.Context, host Host, path string, query map[string]any, out any) error {
	if host == nil {
		return fmt.Errorf("continuum host is not connected")
	}
	resp, err := host.CallPluginHTTP(ctx, runtimehost.CallPluginHTTPRequest{
		InstallationID: s.installationID,
		Method:         http.MethodGet,
		Path:           path,
		Query:          query,
	})
	if err != nil {
		return fmt.Errorf("call %s source: %w", s.mediaType, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s source returned %d: %s", s.mediaType, resp.StatusCode, string(resp.Body))
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("decode %s source: %w", s.mediaType, err)
	}
	return nil
}

func sourceForType(sources []CatalogSource, mediaType string) CatalogSource {
	for _, src := range sources {
		if src != nil && src.MediaType() == mediaType {
			return src
		}
	}
	return nil
}
