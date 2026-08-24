package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) testSourceConnection(source *listsyncv1.ListSource) (message string, itemCount int, err error) {
	switch source.GetType() {
	case "trakt":
		return m.testTrakt(source)
	case "imdb":
		return m.testIMDb(source)
	case "plex":
		return m.testPlex(source)
	case "jellyfin":
		return m.testJellyfin(source)
	case "radarr":
		return m.testRadarr(source)
	case "sonarr":
		return m.testSonarr(source)
	case "radarr_importlist":
		return m.testRadarrImportList(source)
	case "sonarr_importlist":
		return m.testSonarrImportList(source)
	case "trakt_list":
		return m.testTraktList(source)
	case "stevenlu":
		return m.testStevenLu(source)
	case "tmdb_list":
		return m.testTMDBList(source)
	case "rss":
		return m.testRSS(source)
	case "watcharr":
		return m.testWatcharr(source)
	case "lastfm":
		return m.testLastfm(source)
	default:
		return "", 0, fmt.Errorf("unknown source type: %s", source.GetType())
	}
}

func (m *Module) testTrakt(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetClientId() == "" {
		return "", 0, fmt.Errorf("trakt client_id required")
	}
	if source.GetApiKey() == "" {
		return "", 0, fmt.Errorf("trakt api_key (OAuth access token) required")
	}
	items, err := m.fetchTrakt(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Trakt watchlist reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testIMDb(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetListUrl() == "" {
		return "", 0, fmt.Errorf("imdb list_url required")
	}
	items, err := m.fetchIMDb(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("IMDb list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testPlex(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetApiKey() == "" {
		return "", 0, fmt.Errorf("plex api_key (token) required")
	}
	items, err := m.fetchPlex(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Plex watchlist reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testJellyfin(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetBaseUrl() == "" {
		return "", 0, fmt.Errorf("jellyfin base_url required")
	}
	if source.GetApiKey() == "" {
		return "", 0, fmt.Errorf("jellyfin api_key required")
	}
	items, err := m.fetchJellyfin(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Jellyfin list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testRadarr(source *listsyncv1.ListSource) (string, int, error) {
	version, err := m.arrSystemStatus(source.GetBaseUrl(), source.GetApiKey())
	if err != nil {
		return "", 0, err
	}
	items, err := m.fetchRadarr(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Radarr %s reachable (%d monitored movies)", version, len(items)), len(items), nil
}

func (m *Module) testSonarr(source *listsyncv1.ListSource) (string, int, error) {
	version, err := m.arrSystemStatus(source.GetBaseUrl(), source.GetApiKey())
	if err != nil {
		return "", 0, err
	}
	items, err := m.fetchSonarr(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Sonarr %s reachable (%d monitored series)", version, len(items)), len(items), nil
}

func (m *Module) testRadarrImportList(source *listsyncv1.ListSource) (string, int, error) {
	version, err := m.arrSystemStatus(source.GetBaseUrl(), source.GetApiKey())
	if err != nil {
		return "", 0, err
	}
	items, err := m.fetchRadarrImportList(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Radarr %s import lists reachable (%d pending movies)", version, len(items)), len(items), nil
}

func (m *Module) testSonarrImportList(source *listsyncv1.ListSource) (string, int, error) {
	version, err := m.arrSystemStatus(source.GetBaseUrl(), source.GetApiKey())
	if err != nil {
		return "", 0, err
	}
	items, err := m.fetchSonarrImportList(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Sonarr %s import lists reachable (%d pending series)", version, len(items)), len(items), nil
}

func (m *Module) testTraktList(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetClientId() == "" {
		return "", 0, fmt.Errorf("trakt client_id required")
	}
	if source.GetListUrl() == "" {
		return "", 0, fmt.Errorf("trakt list_url required")
	}
	items, err := m.fetchTraktList(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Trakt list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testStevenLu(source *listsyncv1.ListSource) (string, int, error) {
	items, err := m.fetchStevenLu(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("StevenLu list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testTMDBList(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetListUrl() == "" {
		return "", 0, fmt.Errorf("tmdb list_url (list id) required")
	}
	if source.GetApiKey() == "" {
		return "", 0, fmt.Errorf("tmdb api_key required")
	}
	items, err := m.fetchTMDBList(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("TMDB list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testRSS(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetListUrl() == "" {
		return "", 0, fmt.Errorf("rss list_url (feed url) required")
	}
	items, err := m.fetchRSS(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("RSS feed reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) testWatcharr(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetBaseUrl() == "" {
		return "", 0, fmt.Errorf("watcharr base_url required")
	}
	if source.GetApiKey() == "" && source.GetUsername() == "" {
		return "", 0, fmt.Errorf("watcharr api_key (JWT) or username + api_key (password) required")
	}
	items, err := m.fetchWatcharr(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Watcharr list reachable (%d items)", len(items)), len(items), nil
}

func (m *Module) arrSystemStatus(baseURL, apiKey string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("base_url required")
	}
	if apiKey == "" {
		return "", fmt.Errorf("api_key required")
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v3/system/status", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("arr status: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("arr API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	var status struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return "", fmt.Errorf("parse arr status: %w", err)
	}
	if status.Version == "" {
		return "unknown", nil
	}
	return status.Version, nil
}

func (m *Module) testLastfm(source *listsyncv1.ListSource) (string, int, error) {
	if source.GetApiKey() == "" {
		return "", 0, fmt.Errorf("lastfm api_key required")
	}
	items, err := m.fetchLastfm(source)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("Last.fm reachable (%d artists)", len(items)), len(items), nil
}

func probeSourceFromRequest(req *listsyncv1.TestSourceRequest) *listsyncv1.ListSource {
	return &listsyncv1.ListSource{
		Type:     req.GetType(),
		Username: req.GetUsername(),
		ClientId: req.GetClientId(),
		ListUrl:  req.GetListUrl(),
		BaseUrl:  req.GetBaseUrl(),
		ApiKey:   req.GetApiKey(),
	}
}
