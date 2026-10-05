package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchJellyfin(source *listsyncv1.ListSource) ([]syncItem, error) {
	baseURL := strings.TrimRight(source.GetBaseUrl(), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("jellyfin base_url not configured for source %s", source.GetName())
	}
	apiKey := source.GetApiKey()
	if apiKey == "" {
		return nil, fmt.Errorf("jellyfin api_key not configured for source %s", source.GetName())
	}

	if err := validateIntegrationURL(baseURL); err != nil {
		return nil, err
	}
	var endpoint string
	if playlistID := strings.TrimSpace(source.GetListUrl()); playlistID != "" {
		endpoint = fmt.Sprintf("%s/Playlists/%s/Items?Fields=ProviderIds", baseURL, url.PathEscape(playlistID))
	} else {
		userID := strings.TrimSpace(source.GetUsername())
		if userID == "" {
			return nil, fmt.Errorf("jellyfin username (userId) required for favorites sync on source %s", source.GetName())
		}
		q := url.Values{}
		q.Set("Filters", "IsFavorite")
		q.Set("IncludeItemTypes", "Movie,Series")
		q.Set("Recursive", "true")
		q.Set("Fields", "ProviderIds")
		endpoint = fmt.Sprintf("%s/Users/%s/Items?%s", baseURL, url.PathEscape(userID), q.Encode())
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jellyfin fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jellyfin API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return parseJellyfinItems(body)
}

func parseJellyfinItems(body []byte) ([]syncItem, error) {
	type jfItem struct {
		ID             string            `json:"Id"`
		Name           string            `json:"Name"`
		Type           string            `json:"Type"`
		ProductionYear int               `json:"ProductionYear"`
		ProviderIds    map[string]string `json:"ProviderIds"`
	}
	var raw struct {
		Items []jfItem `json:"Items"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		var arr []jfItem
		if err2 := json.Unmarshal(body, &arr); err2 != nil {
			return nil, fmt.Errorf("parse jellyfin: %w", err)
		}
		raw.Items = arr
	}

	var items []syncItem
	for _, it := range raw.Items {
		mediaType := "movie"
		if it.Type == "Series" {
			mediaType = "tv"
		}
		tmdbID := 0
		imdbID := ""
		if it.ProviderIds != nil {
			if v := it.ProviderIds["Tmdb"]; v != "" {
				tmdbID, _ = strconv.Atoi(v)
			}
			if v := it.ProviderIds["Imdb"]; v != "" {
				imdbID = v
			}
		}
		if it.ID == "" {
			continue
		}
		items = append(items, syncItem{
			ExternalID: "jf_" + it.ID,
			IMDBID:     imdbID,
			TMDBID:     tmdbID,
			MediaType:  mediaType,
			Title:      it.Name,
			Year:       it.ProductionYear,
			Action:     "watchlist",
		})
	}
	return items, nil
}
