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

type arrImportListField struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type arrImportListConfig struct {
	ID                 int                  `json:"id"`
	Name               string               `json:"name"`
	Implementation     string               `json:"implementation"`
	Enabled            bool                 `json:"enabled"`
	EnableAutomaticAdd bool                 `json:"enableAutomaticAdd"`
	Fields             []arrImportListField `json:"fields"`
}

func (m *Module) fetchRadarrImportList(source *listsyncv1.ListSource) ([]syncItem, error) {
	m.refreshArrImportListCache(source)
	body, err := m.arrAPIGet(source, "/api/v3/importlist/movie")
	if err != nil {
		return nil, err
	}
	listID, _ := arrImportListIDFilter(source.GetListUrl())
	return parseArrImportListMovies(body, "radarr_importlist", listID)
}

func (m *Module) fetchSonarrImportList(source *listsyncv1.ListSource) ([]syncItem, error) {
	m.refreshArrImportListCache(source)
	listID, _ := arrImportListIDFilter(source.GetListUrl())
	body, err := m.arrAPIGet(source, "/api/v3/importlist/series")
	if err == nil {
		return parseArrImportListSeries(body, "sonarr_importlist", listID)
	}
	if !arrEndpointMissing(err) {
		return nil, err
	}
	return m.fetchSonarrImportListViaConfigs(source)
}

func (m *Module) fetchSonarrImportListViaConfigs(source *listsyncv1.ListSource) ([]syncItem, error) {
	body, err := m.arrAPIGet(source, "/api/v3/importlist")
	if err != nil {
		return nil, fmt.Errorf("sonarr importlist configs: %w", err)
	}
	configs, err := parseArrImportListConfigs(body)
	if err != nil {
		return nil, err
	}
	wantListID, hasListID := arrImportListIDFilter(source.GetListUrl())
	var out []syncItem
	seen := make(map[string]struct{})
	for _, cfg := range configs {
		if !cfg.Enabled || !cfg.EnableAutomaticAdd {
			continue
		}
		if hasListID && cfg.ID != wantListID {
			continue
		}
		items, fetchErr := m.fetchItemsFromArrImportListConfig(source, cfg)
		if fetchErr != nil {
			return nil, fmt.Errorf("import list %q (%s): %w", cfg.Name, cfg.Implementation, fetchErr)
		}
		for _, item := range items {
			if _, ok := seen[item.ExternalID]; ok {
				continue
			}
			seen[item.ExternalID] = struct{}{}
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *Module) fetchItemsFromArrImportListConfig(parent *listsyncv1.ListSource, cfg arrImportListConfig) ([]syncItem, error) {
	proxy := arrImportListProxySource(parent, cfg)
	impl := strings.ToLower(cfg.Implementation)
	switch {
	case strings.Contains(impl, "traktlist"), strings.Contains(impl, "traktlistimport"):
		return m.fetchTraktList(proxy)
	case strings.Contains(impl, "traktuser"), strings.Contains(impl, "traktwatchlist"):
		return m.fetchTrakt(proxy)
	case strings.Contains(impl, "trakt"):
		return m.fetchTraktList(proxy)
	case strings.Contains(impl, "stevenlu"):
		return m.fetchStevenLu(proxy)
	case strings.Contains(impl, "tmdb"):
		return m.fetchTMDBList(proxy)
	case strings.Contains(impl, "rss"):
		return m.fetchRSS(proxy)
	case strings.Contains(impl, "imdb"):
		return m.fetchIMDb(proxy)
	case strings.Contains(impl, "plex"):
		return m.fetchPlex(proxy)
	case strings.Contains(impl, "jellyfin"):
		return m.fetchJellyfin(proxy)
	case strings.Contains(impl, "radarr"):
		return m.fetchRadarr(proxy)
	case strings.Contains(impl, "sonarr"):
		return m.fetchSonarr(proxy)
	case strings.Contains(impl, "watcharr"):
		return m.fetchWatcharr(proxy)
	default:
		return nil, fmt.Errorf("unsupported import list implementation %q", cfg.Implementation)
	}
}

func arrImportListProxySource(parent *listsyncv1.ListSource, cfg arrImportListConfig) *listsyncv1.ListSource {
	fields := cfg.Fields
	proxy := &listsyncv1.ListSource{
		Name:     cfg.Name,
		BaseUrl:  arrFieldValue(fields, "baseUrl", "baseURL", "urlBase"),
		ApiKey:   arrFieldValue(fields, "apiKey", "accessToken", "refreshToken", "token"),
		ClientId: arrFieldValue(fields, "clientId", "clientID"),
		Username: arrFieldValue(fields, "authUser", "username", "signIn"),
		ListUrl:  arrFieldValue(fields, "listId", "listname", "listName", "link", "url", "listUrl"),
	}
	if proxy.GetBaseUrl() == "" {
		proxy.BaseUrl = parent.GetBaseUrl()
	}
	if proxy.GetApiKey() == "" {
		proxy.ApiKey = parent.GetApiKey()
	}
	if proxy.GetClientId() == "" {
		proxy.ClientId = parent.GetClientId()
	}
	if proxy.GetListUrl() == "" {
		proxy.ListUrl = parent.GetListUrl()
	}
	if tmdbType := arrFieldValue(fields, "tmdbListType", "listType"); tmdbType != "" {
		switch tmdbType {
		case "1", "popular":
			proxy.ListUrl = "movie/popular"
		case "2", "top":
			proxy.ListUrl = "movie/top_rated"
		}
	}
	return proxy
}

func arrFieldValue(fields []arrImportListField, names ...string) string {
	for _, want := range names {
		want = strings.ToLower(want)
		for _, f := range fields {
			if strings.ToLower(f.Name) != want {
				continue
			}
			switch v := f.Value.(type) {
			case string:
				return strings.TrimSpace(v)
			case float64:
				return strconv.Itoa(int(v))
			case bool:
				if v {
					return "true"
				}
				return "false"
			default:
				if f.Value != nil {
					return strings.TrimSpace(fmt.Sprint(f.Value))
				}
			}
		}
	}
	return ""
}

func (m *Module) arrAPIGet(source *listsyncv1.ListSource, path string) ([]byte, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(source.GetBaseUrl()), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base_url required for source %s", source.GetName())
	}
	apiKey := strings.TrimSpace(source.GetApiKey())
	if apiKey == "" {
		return nil, fmt.Errorf("api_key required for source %s", source.GetName())
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("arr endpoint missing (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arr API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return body, nil
}

func arrEndpointMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "missing")
}

func parseArrImportListConfigs(body []byte) ([]arrImportListConfig, error) {
	var configs []arrImportListConfig
	if err := json.Unmarshal(body, &configs); err != nil {
		return nil, fmt.Errorf("parse importlist configs: %w", err)
	}
	return configs, nil
}

func parseArrImportListMovies(body []byte, prefix string, listIDFilter int) ([]syncItem, error) {
	var rows []struct {
		Title  string `json:"title"`
		TmdbID int    `json:"tmdbId"`
		ImdbID string `json:"imdbId"`
		Year   int    `json:"year"`
		ListID int    `json:"listId"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("parse importlist movies: %w", err)
	}
	out := make([]syncItem, 0, len(rows))
	for _, row := range rows {
		if listIDFilter > 0 && row.ListID != listIDFilter {
			continue
		}
		if row.TmdbID <= 0 && row.ImdbID == "" {
			continue
		}
		extID := fmt.Sprintf("%s_%d", prefix, row.TmdbID)
		if row.TmdbID <= 0 {
			extID = fmt.Sprintf("%s_%s", prefix, row.ImdbID)
		}
		if row.ListID > 0 {
			extID = fmt.Sprintf("%s_list%d_%s", prefix, row.ListID, strings.TrimPrefix(extID, prefix+"_"))
		}
		out = append(out, syncItem{
			ExternalID: extID,
			TMDBID:     row.TmdbID,
			IMDBID:     row.ImdbID,
			MediaType:  "movie",
			Title:      row.Title,
			Year:       row.Year,
			Action:     "list",
		})
	}
	return out, nil
}

func parseArrImportListSeries(body []byte, prefix string, listIDFilter int) ([]syncItem, error) {
	var rows []struct {
		Title  string `json:"title"`
		TmdbID int    `json:"tmdbId"`
		TvdbID int    `json:"tvdbId"`
		ImdbID string `json:"imdbId"`
		Year   int    `json:"year"`
		ListID int    `json:"listId"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("parse importlist series: %w", err)
	}
	out := make([]syncItem, 0, len(rows))
	for _, row := range rows {
		if listIDFilter > 0 && row.ListID != listIDFilter {
			continue
		}
		if row.TmdbID <= 0 && row.TvdbID <= 0 && row.ImdbID == "" {
			continue
		}
		extID := fmt.Sprintf("%s_%d", prefix, row.TmdbID)
		if row.TmdbID <= 0 && row.TvdbID > 0 {
			extID = fmt.Sprintf("tvdb_%d", row.TvdbID)
		} else if row.TmdbID <= 0 {
			extID = fmt.Sprintf("%s_%s", prefix, row.ImdbID)
		}
		if row.ListID > 0 {
			extID = fmt.Sprintf("%s_list%d_%s", prefix, row.ListID, strings.TrimPrefix(extID, prefix+"_"))
		}
		out = append(out, syncItem{
			ExternalID: extID,
			TMDBID:     row.TmdbID,
			IMDBID:     row.ImdbID,
			MediaType:  "tv",
			Title:      row.Title,
			Year:       row.Year,
			Action:     "list",
		})
	}
	return out, nil
}

func arrImportListIDFilter(listURL string) (int, bool) {
	listURL = strings.TrimSpace(listURL)
	if listURL == "" {
		return 0, false
	}
	if strings.HasPrefix(listURL, "list:") {
		listURL = strings.TrimPrefix(listURL, "list:")
	}
	id, err := strconv.Atoi(listURL)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
