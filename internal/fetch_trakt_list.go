package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchTraktList(source *listsyncv1.ListSource) ([]syncItem, error) {
	clientID := strings.TrimSpace(source.GetClientId())
	if clientID == "" {
		clientID = os.Getenv("TRAKT_CLIENT_ID")
	}
	if clientID == "" {
		return nil, fmt.Errorf("trakt client_id required for trakt_list source %s", source.GetName())
	}
	apiKey := strings.TrimSpace(source.GetApiKey())
	slug := traktListSlug(source.GetListUrl())
	if slug == "" {
		return nil, fmt.Errorf("trakt list url required for source %s", source.GetName())
	}
	req, err := http.NewRequest("GET", "https://api.trakt.tv/lists/"+slug+"/items?type=movie,show", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", clientID)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trakt list API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	var items []struct {
		Type  string `json:"type"`
		Movie *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				Trakt int    `json:"trakt"`
				TMDB  int    `json:"tmdb"`
				IMDB  string `json:"imdb"`
			} `json:"ids"`
		} `json:"movie"`
		Show *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
			IDs   struct {
				Trakt int    `json:"trakt"`
				TMDB  int    `json:"tmdb"`
				IMDB  string `json:"imdb"`
			} `json:"ids"`
		} `json:"show"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	var out []syncItem
	for _, it := range items {
		switch {
		case it.Movie != nil:
			mv := it.Movie
			out = append(out, syncItem{
				ExternalID: fmt.Sprintf("trakt_list_%d", mv.IDs.Trakt),
				TMDBID:     mv.IDs.TMDB,
				IMDBID:     mv.IDs.IMDB,
				MediaType:  "movie",
				Title:      mv.Title,
				Year:       mv.Year,
				Action:     "list",
			})
		case it.Show != nil:
			show := it.Show
			out = append(out, syncItem{
				ExternalID: fmt.Sprintf("trakt_list_%d", show.IDs.Trakt),
				TMDBID:     show.IDs.TMDB,
				IMDBID:     show.IDs.IMDB,
				MediaType:  "tv",
				Title:      show.Title,
				Year:       show.Year,
				Action:     "list",
			})
		}
	}
	return out, nil
}

func traktListSlug(listURL string) string {
	listURL = strings.TrimSpace(listURL)
	if listURL == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(listURL, "/"), "/")
	for i, p := range parts {
		if p == "lists" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return parts[len(parts)-1]
}
