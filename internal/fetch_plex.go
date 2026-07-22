package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchPlex(source *listsyncv1.ListSource) ([]syncItem, error) {
	token := source.GetApiKey()
	if token == "" {
		return nil, fmt.Errorf("plex api_key (X-Plex-Token) not configured for source %s", source.GetName())
	}
	clientID := source.GetClientId()
	if clientID == "" {
		clientID = "muxcore-media-list-sync"
	}

	var items []syncItem
	start := 0
	const pageSize = 50
	for {
		page, err := m.fetchPlexPage(token, clientID, start, pageSize)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
		if len(page) < pageSize {
			break
		}
		start += pageSize
	}
	return items, nil
}

func (m *Module) fetchPlexPage(token, clientID string, start, size int) ([]syncItem, error) {
	u := fmt.Sprintf("https://discover.provider.plex.tv/library/sections/watchlist/all?X-Plex-Container-Start=%d&X-Plex-Container-Size=%d", start, size)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", token)
	req.Header.Set("X-Plex-Client-Identifier", clientID)

	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plex watchlist: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plex API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return parsePlexWatchlist(body)
}

func parsePlexWatchlist(body []byte) ([]syncItem, error) {
	var raw struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey string `json:"ratingKey"`
				Title     string `json:"title"`
				Type      string `json:"type"`
				Year      int    `json:"year"`
				Guid      string `json:"guid"`
				GuidList  []struct {
					ID string `json:"id"`
				} `json:"Guid"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse plex: %w", err)
	}

	var items []syncItem
	for _, md := range raw.MediaContainer.Metadata {
		mediaType := "movie"
		if md.Type == "show" {
			mediaType = "tv"
		}
		tmdbID, imdbID := 0, ""
		for _, g := range md.GuidList {
			id := g.ID
			switch {
			case strings.HasPrefix(id, "tmdb://"):
				tmdbID, _ = strconv.Atoi(strings.TrimPrefix(id, "tmdb://"))
			case strings.HasPrefix(id, "imdb://"):
				imdbID = strings.TrimPrefix(id, "imdb://")
			}
		}
		if tmdbID == 0 && strings.HasPrefix(md.Guid, "tmdb://") {
			tmdbID, _ = strconv.Atoi(strings.TrimPrefix(md.Guid, "tmdb://"))
		}
		if imdbID == "" && strings.HasPrefix(md.Guid, "imdb://") {
			imdbID = strings.TrimPrefix(md.Guid, "imdb://")
		}
		extID := md.RatingKey
		if extID == "" {
			extID = md.Guid
		}
		if extID == "" {
			continue
		}
		items = append(items, syncItem{
			ExternalID: "plex_" + extID,
			IMDBID:     imdbID,
			TMDBID:     tmdbID,
			MediaType:  mediaType,
			Title:      md.Title,
			Year:       md.Year,
			Action:     "watchlist",
		})
	}
	return items, nil
}
