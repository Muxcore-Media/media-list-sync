package internal

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

var (
	rssIMDBPattern     = regexp.MustCompile(`(?i)\b(tt\d{7,8})\b`)
	rssTMDBPattern     = regexp.MustCompile(`(?i)themoviedb\.org/(?:movie|tv)/(\d+)`)
	rssIMDBTagPattern  = regexp.MustCompile(`(?i)\{?\s*imdb\s*:\s*(tt\d{7,8})\s*\}?`)
	rssTMDBTagPattern  = regexp.MustCompile(`(?i)\{?\s*tmdb\s*:\s*(\d+)\s*\}?`)
	rssTitleTagPattern = regexp.MustCompile(`(?i)\{(?:imdb|tmdb)\s*:[^}]+\}`)
)

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
}

func (m *Module) fetchRSS(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	feedURL := strings.TrimSpace(source.GetListUrl())
	if feedURL == "" {
		feedURL = strings.TrimSpace(source.GetBaseUrl())
	}
	if feedURL == "" {
		return nil, fmt.Errorf("rss feed url required in list_url for source %s", source.GetName())
	}
	resp, err := m.httpGet(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rss feed %d: %s", resp.StatusCode, string(body[:minInt(len(body), 200)]))
	}
	return parseRSSFeed(body)
}

func parseRSSFeed(body []byte) ([]syncItem, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	var out []syncItem
	seen := make(map[string]struct{})
	for i, row := range feed.Channel.Items {
		item, ok := rssItemFromEntry(row, i)
		if !ok {
			continue
		}
		if _, dup := seen[item.ExternalID]; dup {
			continue
		}
		seen[item.ExternalID] = struct{}{}
		out = append(out, item)
	}
	return out, nil
}

func rssItemFromEntry(row rssItem, index int) (syncItem, bool) {
	blob := strings.Join([]string{row.Title, row.Link, row.Description, row.GUID}, " ")
	imdb := ""
	if m := rssIMDBTagPattern.FindStringSubmatch(blob); len(m) > 1 {
		imdb = m[1]
	}
	if imdb == "" {
		if m := rssIMDBPattern.FindStringSubmatch(blob); len(m) > 1 {
			imdb = m[1]
		}
	}
	tmdbID := 0
	mediaType := "movie"
	if m := rssTMDBTagPattern.FindStringSubmatch(blob); len(m) > 1 {
		tmdbID = atoi(m[1])
	}
	if tmdbID == 0 {
		if m := rssTMDBPattern.FindStringSubmatch(blob); len(m) > 1 {
			tmdbID = atoi(m[1])
		}
	}
	if strings.Contains(strings.ToLower(blob), "/tv/") || strings.Contains(strings.ToLower(blob), "tmdb:tv") {
		mediaType = "tv"
	}
	title := strings.TrimSpace(row.Title)
	title = rssTitleTagPattern.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSpace(row.Link)
	}
	externalID := strings.TrimSpace(row.GUID)
	if externalID == "" {
		externalID = strings.TrimSpace(row.Link)
	}
	if externalID == "" {
		externalID = fmt.Sprintf("rss_%d", index)
	}
	if !strings.HasPrefix(externalID, "rss_") {
		externalID = "rss_" + externalID
	}
	if tmdbID <= 0 && imdb == "" {
		return syncItem{}, false
	}
	return syncItem{
		ExternalID: externalID,
		TMDBID:     tmdbID,
		IMDBID:     imdb,
		MediaType:  mediaType,
		Title:      title,
		Action:     "list",
	}, true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
