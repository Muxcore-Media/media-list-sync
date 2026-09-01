package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

const defaultTraktAPIBase = "https://api.trakt.tv"

type traktIDs struct {
	IMDb  string `json:"imdb"`
	Trakt int    `json:"trakt"`
	TMDB  int    `json:"tmdb"`
}

type traktMovie struct {
	Title string   `json:"title"`
	IDs   traktIDs `json:"ids"`
	Year  int      `json:"year"`
}

type traktShow struct {
	Title string   `json:"title"`
	IDs   traktIDs `json:"ids"`
	Year  int      `json:"year"`
}

type traktWatchlistMovie struct {
	Movie traktMovie `json:"movie"`
}

type traktWatchlistShow struct {
	Show traktShow `json:"show"`
}

type traktCollectionMovie struct {
	Movie traktMovie `json:"movie"`
}

type traktCollectionShow struct {
	Show traktShow `json:"show"`
}

type traktWatchedMovie struct {
	Movie traktMovie `json:"movie"`
}

type traktWatchedShow struct {
	Show traktShow `json:"show"`
}

func (m *Module) traktAPIBaseURL() string {
	if b := strings.TrimSpace(m.traktAPIBase); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultTraktAPIBase
}

func parseTraktListKind(listURL string) string {
	listURL = strings.ToLower(strings.TrimSpace(listURL))
	switch listURL {
	case "collection", "watched", "watchlist":
		return listURL
	default:
		return "watchlist"
	}
}

func (m *Module) fetchTrakt(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	clientID := source.GetClientId()
	if clientID == "" {
		return nil, fmt.Errorf("trakt client_id not configured for source %s", source.GetName())
	}
	accessToken := source.GetApiKey()
	if accessToken == "" {
		return nil, fmt.Errorf("trakt api_key (OAuth access token) not configured for source %s", source.GetName())
	}

	kind := parseTraktListKind(source.GetListUrl())
	action := kind
	if action == "" {
		action = "watchlist"
	}

	var items []syncItem

	switch kind {
	case "collection":
		movies, movieErr := traktGetPaginated[traktCollectionMovie](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/collection/movies")
		if movieErr != nil {
			return nil, fmt.Errorf("trakt collection movies: %w", movieErr)
		}
		for _, entry := range movies {
			items = append(items, traktMovieItem(entry.Movie, action))
		}
		shows, showErr := traktGetPaginated[traktCollectionShow](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/collection/shows")
		if showErr != nil {
			return nil, fmt.Errorf("trakt collection shows: %w", showErr)
		}
		for _, entry := range shows {
			items = append(items, traktShowItem(entry.Show, action))
		}
	case "watched":
		movies, movieErr := traktGetPaginated[traktWatchedMovie](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/watched/movies")
		if movieErr != nil {
			return nil, fmt.Errorf("trakt watched movies: %w", movieErr)
		}
		for _, entry := range movies {
			items = append(items, traktMovieItem(entry.Movie, action))
		}
		shows, showErr := traktGetPaginated[traktWatchedShow](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/watched/shows")
		if showErr != nil {
			return nil, fmt.Errorf("trakt watched shows: %w", showErr)
		}
		for _, entry := range shows {
			items = append(items, traktShowItem(entry.Show, action))
		}
	default:
		movies, movieErr := traktGetPaginated[traktWatchlistMovie](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/watchlist/movies")
		if movieErr != nil {
			return nil, fmt.Errorf("trakt watchlist movies: %w", movieErr)
		}
		for _, entry := range movies {
			items = append(items, traktMovieItem(entry.Movie, action))
		}
		shows, showErr := traktGetPaginated[traktWatchlistShow](ctx, m.httpCli, m.traktAPIBaseURL(), clientID, accessToken, "/sync/watchlist/shows")
		if showErr != nil {
			return nil, fmt.Errorf("trakt watchlist shows: %w", showErr)
		}
		for _, entry := range shows {
			items = append(items, traktShowItem(entry.Show, action))
		}
	}

	return items, nil
}

func traktMovieItem(mv traktMovie, action string) syncItem {
	if mv.IDs.Trakt == 0 && mv.Title == "" {
		return syncItem{}
	}
	return syncItem{
		ExternalID: fmt.Sprintf("trakt_%d", mv.IDs.Trakt),
		IMDBID:     mv.IDs.IMDb,
		TMDBID:     mv.IDs.TMDB,
		MediaType:  "movie",
		Title:      mv.Title,
		Year:       mv.Year,
		Action:     action,
	}
}

func traktShowItem(show traktShow, action string) syncItem {
	if show.IDs.Trakt == 0 && show.Title == "" {
		return syncItem{}
	}
	return syncItem{
		ExternalID: fmt.Sprintf("trakt_%d", show.IDs.Trakt),
		IMDBID:     show.IDs.IMDb,
		TMDBID:     show.IDs.TMDB,
		MediaType:  "tv",
		Title:      show.Title,
		Year:       show.Year,
		Action:     action,
	}
}

func traktGetPaginated[T any](ctx context.Context, cli *http.Client, baseURL, clientID, accessToken, path string) ([]T, error) {
	page := 1
	var all []T
	for {
		pagePath := path
		if page > 1 {
			pagePath = fmt.Sprintf("%s?page=%d", path, page)
		}
		batch, pageCount, err := traktGetPage[T](ctx, cli, baseURL, clientID, accessToken, pagePath)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if pageCount <= 0 || page >= pageCount {
			break
		}
		page++
	}
	return all, nil
}

func traktGetPage[T any](ctx context.Context, cli *http.Client, baseURL, clientID, accessToken, path string) ([]T, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, http.NoBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", clientID)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := cli.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("trakt API %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}

	var result []T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("parse: %w", err)
	}

	pageCount := 1
	if v := resp.Header.Get("X-Pagination-Page-Count"); v != "" {
		if n, parseErr := strconv.Atoi(v); parseErr == nil && n > 0 {
			pageCount = n
		}
	}
	return result, pageCount, nil
}
