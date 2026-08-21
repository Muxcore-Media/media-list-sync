package internal

import (
	"strings"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestParsePlexWatchlist(t *testing.T) {
	body := []byte(`{
		"MediaContainer": {
			"Metadata": [
				{
					"ratingKey": "1",
					"title": "Fight Club",
					"type": "movie",
					"year": 1999,
					"Guid": [
						{"id": "tmdb://550"},
						{"id": "imdb://tt0137523"}
					]
				},
				{
					"ratingKey": "2",
					"title": "Breaking Bad",
					"type": "show",
					"year": 2008,
					"Guid": [{"id": "tmdb://1396"}]
				}
			]
		}
	}`)
	items, err := parsePlexWatchlist(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].TMDBID != 550 || items[0].IMDBID != "tt0137523" || items[0].MediaType != "movie" {
		t.Errorf("unexpected movie item: %+v", items[0])
	}
	if items[1].TMDBID != 1396 || items[1].MediaType != "tv" {
		t.Errorf("unexpected tv item: %+v", items[1])
	}
}

func TestParseJellyfinItems(t *testing.T) {
	body := []byte(`{
		"Items": [
			{
				"Id": "abc",
				"Name": "Fight Club",
				"Type": "Movie",
				"ProductionYear": 1999,
				"ProviderIds": {"Tmdb": "550", "Imdb": "tt0137523"}
			},
			{
				"Id": "def",
				"Name": "Breaking Bad",
				"Type": "Series",
				"ProductionYear": 2008,
				"ProviderIds": {"Tmdb": "1396"}
			}
		]
	}`)
	items, err := parseJellyfinItems(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2, got %d", len(items))
	}
	if items[0].TMDBID != 550 || items[0].MediaType != "movie" {
		t.Errorf("unexpected: %+v", items[0])
	}
	if items[1].MediaType != "tv" || items[1].TMDBID != 1396 {
		t.Errorf("unexpected: %+v", items[1])
	}
}

func TestParseRadarrMovies(t *testing.T) {
	body := []byte(`[
		{"id": 1, "title": "Fight Club", "year": 1999, "tmdbId": 550, "imdbId": "tt0137523", "monitored": true},
		{"id": 2, "title": "Skipped", "year": 2000, "tmdbId": 1, "monitored": false}
	]`)
	items, err := parseRadarrMovies(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 monitored, got %d", len(items))
	}
	if items[0].TMDBID != 550 || items[0].MediaType != "movie" {
		t.Errorf("unexpected: %+v", items[0])
	}
}

func TestParseSonarrSeries(t *testing.T) {
	body := []byte(`[
		{"id": 10, "title": "Breaking Bad", "year": 2008, "tmdbId": 1396, "imdbId": "tt0903747", "tvdbId": 81189, "monitored": true},
		{"id": 11, "title": "Unmonitored", "year": 2010, "tmdbId": 2, "monitored": false}
	]`)
	items, err := parseSonarrSeries(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1, got %d", len(items))
	}
	if items[0].TMDBID != 1396 || items[0].MediaType != "tv" {
		t.Errorf("unexpected: %+v", items[0])
	}
}

func TestAddSourceWithNewFields(t *testing.T) {
	m := newTestModule(t)
	added, err := m.AddSource(t.Context(), &listsyncv1.AddSourceRequest{
		Name:             "Radarr Home",
		Type:             "radarr",
		BaseUrl:          "http://localhost:7878",
		ApiKey:           "secret",
		QualityProfileId: "qp1",
		RootFolderPath:   "/movies",
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Source.BaseUrl != "http://localhost:7878" {
		t.Errorf("base_url: %s", added.Source.BaseUrl)
	}
	if added.Source.ApiKey != "secret" {
		t.Errorf("api_key: %s", added.Source.ApiKey)
	}
	if added.Source.QualityProfileId != "qp1" || added.Source.RootFolderPath != "/movies" {
		t.Errorf("profile/folder missing: %+v", added.Source)
	}
}

func TestYearFromDate(t *testing.T) {
	if yearFromDate("1999-10-15") != 1999 {
		t.Fatal("expected 1999")
	}
	if yearFromDate("") != 0 {
		t.Fatal("expected 0")
	}
}

func TestParseRSSFeed(t *testing.T) {
	body := []byte(`<?xml version="1.0"?><rss><channel><item>
		<title>Fight Club (1999)</title>
		<link>https://www.imdb.com/title/tt0137523/</link>
		<guid>imdb-tt0137523</guid>
	</item><item>
		<title>Breaking Bad</title>
		<link>https://www.themoviedb.org/tv/1396</link>
	</item><item>
		<title>Alien {imdb:tt0078748}</title>
		<link>https://example.com/alien</link>
	</item></channel></rss>`)
	items, err := parseRSSFeed(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].IMDBID != "tt0137523" || items[0].MediaType != "movie" {
		t.Errorf("imdb item: %+v", items[0])
	}
	if items[1].TMDBID != 1396 || items[1].MediaType != "tv" {
		t.Errorf("tmdb item: %+v", items[1])
	}
	if items[2].IMDBID != "tt0078748" || items[2].Title != "Alien" {
		t.Errorf("tagged title item: %+v", items[2])
	}
}

func TestTMDBListURL(t *testing.T) {
	url, kind := tmdbListURL("trending/movie/week", "key")
	if kind != "movie" || !strings.Contains(url, "/trending/movie/week") {
		t.Fatalf("unexpected trending url: %s kind=%s", url, kind)
	}
	url, kind = tmdbListURL("12345", "key")
	if kind != "" || !strings.Contains(url, "/list/12345") {
		t.Fatalf("unexpected list url: %s kind=%s", url, kind)
	}
}
