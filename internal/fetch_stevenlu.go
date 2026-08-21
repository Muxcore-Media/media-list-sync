package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) fetchStevenLu(_ *listsyncv1.ListSource) ([]syncItem, error) {
	resp, err := m.httpCli.Get("https://api.stevenlu.com/tmdb/movies")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stevenlu API %d", resp.StatusCode)
	}
	var rows []struct {
		IMDBID string  `json:"imdbId"`
		TMDBID float64 `json:"tmdbId"`
		Title  string  `json:"title"`
		Year   float64 `json:"year"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	var out []syncItem
	for i, row := range rows {
		tmdb := int(row.TMDBID)
		if tmdb <= 0 {
			continue
		}
		year := int(row.Year)
		out = append(out, syncItem{
			ExternalID: fmt.Sprintf("stevenlu_%d", tmdb),
			TMDBID:     tmdb,
			IMDBID:     row.IMDBID,
			MediaType:  "movie",
			Title:      row.Title,
			Year:       year,
			Action:     "list",
		})
		if i > 5000 {
			break
		}
	}
	return out, nil
}
