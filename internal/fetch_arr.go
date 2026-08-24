package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchRadarr(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	baseURL := strings.TrimRight(source.GetBaseUrl(), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("radarr base_url not configured for source %s", source.GetName())
	}
	apiKey := source.GetApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("radarr api_key not configured for source %s", source.GetName())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v3/movie", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("radarr fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radarr API %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}
	return parseRadarrMovies(body)
}

func parseRadarrMovies(body []byte) ([]syncItem, error) {
	var movies []struct {
		Title     string `json:"title"`
		ImdbID    string `json:"imdbId"`
		ID        int    `json:"id"`
		Year      int    `json:"year"`
		TmdbID    int    `json:"tmdbId"`
		Monitored bool   `json:"monitored"`
	}
	if err := json.Unmarshal(body, &movies); err != nil {
		return nil, fmt.Errorf("parse radarr: %w", err)
	}

	var items []syncItem
	for _, mv := range movies {
		if !mv.Monitored {
			continue
		}
		items = append(items, syncItem{
			ExternalID: fmt.Sprintf("radarr_%d", mv.ID),
			IMDBID:     mv.ImdbID,
			TMDBID:     mv.TmdbID,
			MediaType:  "movie",
			Title:      mv.Title,
			Year:       mv.Year,
			Action:     "watchlist",
		})
	}
	return items, nil
}

func (m *Module) fetchSonarr(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	baseURL := strings.TrimRight(source.GetBaseUrl(), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("sonarr base_url not configured for source %s", source.GetName())
	}
	apiKey := source.GetApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("sonarr api_key not configured for source %s", source.GetName())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v3/series", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sonarr fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sonarr API %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}
	return parseSonarrSeries(body)
}

func parseSonarrSeries(body []byte) ([]syncItem, error) {
	var series []struct {
		Title     string `json:"title"`
		ImdbID    string `json:"imdbId"`
		ID        int    `json:"id"`
		Year      int    `json:"year"`
		TmdbID    int    `json:"tmdbId"`
		TvdbID    int    `json:"tvdbId"`
		Monitored bool   `json:"monitored"`
	}
	if err := json.Unmarshal(body, &series); err != nil {
		return nil, fmt.Errorf("parse sonarr: %w", err)
	}

	var items []syncItem
	for _, s := range series {
		if !s.Monitored {
			continue
		}
		extID := fmt.Sprintf("sonarr_%d", s.ID)
		if s.TvdbID > 0 && s.TmdbID == 0 && s.ImdbID == "" {
			extID = fmt.Sprintf("tvdb_%d", s.TvdbID)
		}
		items = append(items, syncItem{
			ExternalID: extID,
			IMDBID:     s.ImdbID,
			TMDBID:     s.TmdbID,
			MediaType:  "tv",
			Title:      s.Title,
			Year:       s.Year,
			Action:     "watchlist",
		})
	}
	return items, nil
}
