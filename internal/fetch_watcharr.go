package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchWatcharr(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	token, err := m.watcharrAuthToken(ctx, source)
	if err != nil {
		return nil, err
	}
	mode, arg := parseWatcharrListURL(source.GetListUrl())
	switch mode {
	case "tag":
		return m.fetchWatcharrPaged(ctx, source, token, "/api/tag/"+url.PathEscape(strings.TrimSpace(arg))+"/watched", nil)
	case "public":
		userID, username, ok := parseWatcharrPublicTarget(arg, source.GetUsername())
		if !ok {
			return nil, fmt.Errorf("watcharr public list requires list_url public:<userId>/<username>")
		}
		return m.fetchWatcharrPaged(ctx, source, token, fmt.Sprintf("/api/watched/%d/%s", userID, url.PathEscape(username)), nil)
	default:
		var q url.Values
		if mode == "status" && arg != "" {
			q = url.Values{"status": {arg}}
		}
		return m.fetchWatcharrPaged(ctx, source, token, "/api/watched", q)
	}
}

func (m *Module) watcharrAuthToken(ctx context.Context, source *listsyncv1.ListSource) (string, error) {
	if token := strings.TrimSpace(source.GetApiKey()); token != "" && !looksLikeWatcharrPassword(source) {
		return token, nil
	}
	username := strings.TrimSpace(source.GetUsername())
	password := strings.TrimSpace(source.GetApiKey())
	if username == "" || password == "" {
		return "", fmt.Errorf("watcharr requires api_key JWT or username + api_key password for source %s", source.GetName())
	}
	return m.watcharrLogin(ctx, source, username, password)
}

func looksLikeWatcharrPassword(source *listsyncv1.ListSource) bool {
	// When username is set alongside api_key, treat api_key as password for login.
	return strings.TrimSpace(source.GetUsername()) != "" && strings.TrimSpace(source.GetApiKey()) != ""
}

func (m *Module) watcharrLogin(ctx context.Context, source *listsyncv1.ListSource, username, password string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(source.GetBaseUrl()), "/")
	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/auth/", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return "", fmt.Errorf("watcharr login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("watcharr login %d: %s", resp.StatusCode, string(respBody[:minInt(len(respBody), 200)]))
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", fmt.Errorf("parse watcharr login: %w", err)
	}
	if payload.Token == "" {
		return "", fmt.Errorf("watcharr login returned empty token")
	}
	return payload.Token, nil
}

func parseWatcharrListURL(listURL string) (mode, arg string) {
	listURL = strings.TrimSpace(listURL)
	if listURL == "" {
		return "", ""
	}
	lower := strings.ToLower(listURL)
	switch {
	case strings.HasPrefix(lower, "tag:"):
		return "tag", strings.TrimSpace(listURL[4:])
	case strings.HasPrefix(lower, "public:"):
		return "public", strings.TrimSpace(listURL[7:])
	case strings.HasPrefix(lower, "status:"):
		return "status", strings.TrimSpace(listURL[7:])
	default:
		return "", listURL
	}
}

func parseWatcharrPublicTarget(arg, fallbackUsername string) (userID int, username string, ok bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, "", false
	}
	parts := strings.SplitN(arg, "/", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	id, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || id <= 0 {
		return 0, "", false
	}
	username = strings.TrimSpace(parts[1])
	if username == "" {
		username = strings.TrimSpace(fallbackUsername)
	}
	if username == "" {
		return 0, "", false
	}
	return id, username, true
}

func (m *Module) fetchWatcharrPaged(ctx context.Context, source *listsyncv1.ListSource, token, path string, extra url.Values) ([]syncItem, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(source.GetBaseUrl()), "/")
	// Non-paginated full export when hitting /api/watched without filters.
	if path == "/api/watched" && len(extra) == 0 {
		body, err := m.watcharrGET(ctx, baseURL+path, token)
		if err != nil {
			return nil, err
		}
		return parseWatcharrMediaList(body)
	}

	var out []syncItem
	page := 1
	const pageSize = 100
	for {
		q := url.Values{}
		for k, vals := range extra {
			for _, v := range vals {
				q.Add(k, v)
			}
		}
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", strconv.Itoa(pageSize))
		endpoint := baseURL + path + "?" + q.Encode()
		body, err := m.watcharrGET(ctx, endpoint, token)
		if err != nil {
			return nil, err
		}
		items, totalPages, err := parseWatcharrPagedMedia(body)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if totalPages == 0 || page >= totalPages || len(items) < pageSize {
			break
		}
		page++
	}
	return out, nil
}

func (m *Module) watcharrGET(ctx context.Context, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("watcharr API %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}
	return body, nil
}

func parseWatcharrMediaList(body []byte) ([]syncItem, error) {
	var page []watcharrMedia
	if err := json.Unmarshal(body, &page); err == nil {
		return watcharrMediaToSyncItems(page), nil
	}
	items, _, err := parseWatcharrPagedMedia(body)
	return items, err
}

func parseWatcharrPagedMedia(body []byte) ([]syncItem, int, error) {
	var payload struct {
		Results    []watcharrMedia `json:"results"`
		TotalPages int             `json:"totalPages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, fmt.Errorf("parse watcharr media: %w", err)
	}
	return watcharrMediaToSyncItems(payload.Results), payload.TotalPages, nil
}

//nolint:govet // JSON field order follows Watcharr API schema
type watcharrMedia struct {
	IDs struct {
		TMDB int    `json:"tmdb"`
		IMDB string `json:"imdb"`
	} `json:"ids"`
	Watched struct {
		ID uint `json:"id"`
	} `json:"watched"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	ReleaseDate string `json:"releaseDate"`
}

func watcharrMediaToSyncItems(rows []watcharrMedia) []syncItem {
	var out []syncItem
	for _, row := range rows {
		item, ok := watcharrMediaToSyncItem(row)
		if !ok {
			continue
		}
		out = append(out, item)
	}
	return out
}

func watcharrMediaToSyncItem(row watcharrMedia) (syncItem, bool) {
	mediaType := ""
	switch row.Type {
	case "tmdb_movie":
		mediaType = "movie"
	case "tmdb_tv":
		mediaType = "tv"
	default:
		return syncItem{}, false
	}
	if row.IDs.TMDB <= 0 && row.IDs.IMDB == "" {
		return syncItem{}, false
	}
	extID := fmt.Sprintf("watcharr_%d", row.Watched.ID)
	if row.Watched.ID == 0 {
		if row.IDs.TMDB > 0 {
			extID = fmt.Sprintf("watcharr_tmdb_%d", row.IDs.TMDB)
		} else {
			extID = fmt.Sprintf("watcharr_%s", row.IDs.IMDB)
		}
	}
	year := yearFromDate(row.ReleaseDate)
	return syncItem{
		ExternalID: extID,
		TMDBID:     row.IDs.TMDB,
		IMDBID:     row.IDs.IMDB,
		MediaType:  mediaType,
		Title:      row.Name,
		Year:       year,
		Action:     "list",
	}, true
}
