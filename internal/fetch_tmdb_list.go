package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchTMDBList(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	listID := strings.TrimSpace(source.GetListUrl())
	if listID == "" {
		listID = strings.TrimSpace(source.GetBaseUrl())
	}
	if listID == "" {
		return nil, fmt.Errorf("tmdb list id required in list_url for source %s", source.GetName())
	}
	apiKey := strings.TrimSpace(source.GetApiKey())
	if apiKey == "" {
		apiKey = os.Getenv("TMDB_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("tmdb api key required for source %s", source.GetName())
	}

	url, defaultMediaType := tmdbListURL(listID, apiKey)
	resp, err := m.httpGet(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb list API %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}

	if defaultMediaType != "" {
		return parseTMDBDiscover(body, defaultMediaType)
	}
	return parseTMDBUserList(body)
}

func tmdbListURL(listID, apiKey string) (string, string) {
	key := strings.ToLower(strings.Trim(strings.TrimSpace(listID), "/"))
	switch key {
	case "trending/movie/day":
		return fmt.Sprintf("https://api.themoviedb.org/3/trending/movie/day?api_key=%s", apiKey), "movie"
	case "trending/movie/week":
		return fmt.Sprintf("https://api.themoviedb.org/3/trending/movie/week?api_key=%s", apiKey), "movie"
	case "trending/tv/day":
		return fmt.Sprintf("https://api.themoviedb.org/3/trending/tv/day?api_key=%s", apiKey), "tv"
	case "trending/tv/week":
		return fmt.Sprintf("https://api.themoviedb.org/3/trending/tv/week?api_key=%s", apiKey), "tv"
	case "movie/popular":
		return fmt.Sprintf("https://api.themoviedb.org/3/movie/popular?api_key=%s", apiKey), "movie"
	case "movie/now_playing":
		return fmt.Sprintf("https://api.themoviedb.org/3/movie/now_playing?api_key=%s", apiKey), "movie"
	case "tv/popular":
		return fmt.Sprintf("https://api.themoviedb.org/3/tv/popular?api_key=%s", apiKey), "tv"
	case "tv/on_the_air":
		return fmt.Sprintf("https://api.themoviedb.org/3/tv/on_the_air?api_key=%s", apiKey), "tv"
	default:
		return fmt.Sprintf("https://api.themoviedb.org/3/list/%s?api_key=%s", listID, apiKey), ""
	}
}

func parseTMDBUserList(body []byte) ([]syncItem, error) {
	var payload struct {
		Items []struct {
			MediaType string `json:"media_type"`
			Title     string `json:"title"`
			Name      string `json:"name"`
			Release   string `json:"release_date"`
			FirstAir  string `json:"first_air_date"`
			ID        int    `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var out []syncItem
	for _, row := range payload.Items {
		out = append(out, tmdbRowToSyncItem(row.MediaType, row.ID, row.Title, row.Name, row.Release, row.FirstAir))
	}
	return out, nil
}

func parseTMDBDiscover(body []byte, defaultMediaType string) ([]syncItem, error) {
	var payload struct {
		Results []struct {
			Title    string `json:"title"`
			Name     string `json:"name"`
			Release  string `json:"release_date"`
			FirstAir string `json:"first_air_date"`
			ID       int    `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	var out []syncItem
	for _, row := range payload.Results {
		out = append(out, tmdbRowToSyncItem(defaultMediaType, row.ID, row.Title, row.Name, row.Release, row.FirstAir))
	}
	return out, nil
}

func tmdbRowToSyncItem(mediaType string, id int, title, name, release, firstAir string) syncItem {
	display := title
	if display == "" {
		display = name
	}
	year := yearFromDate(release)
	if year == 0 {
		year = yearFromDate(firstAir)
	}
	if mediaType == "tv" {
		return syncItem{
			ExternalID: fmt.Sprintf("tmdb_list_tv_%d", id),
			TMDBID:     id,
			MediaType:  "tv",
			Title:      display,
			Year:       year,
			Action:     "list",
		}
	}
	return syncItem{
		ExternalID: fmt.Sprintf("tmdb_list_movie_%d", id),
		TMDBID:     id,
		MediaType:  "movie",
		Title:      display,
		Year:       year,
		Action:     "list",
	}
}
