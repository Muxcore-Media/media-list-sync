package internal

import (
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestParseArrImportListMovies(t *testing.T) {
	body := []byte(`[
		{"title":"Fight Club","tmdbId":550,"imdbId":"tt0137523","year":1999,"listId":1},
		{"title":"Other List","tmdbId":1,"year":2000,"listId":2}
	]`)
	items, err := parseArrImportListMovies(body, "radarr_importlist", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 filtered item, got %d", len(items))
	}
	if items[0].TMDBID != 550 || items[0].MediaType != "movie" {
		t.Errorf("unexpected item: %+v", items[0])
	}
}

func TestParseArrImportListSeries(t *testing.T) {
	body := []byte(`[{"title":"Breaking Bad","tmdbId":1396,"tvdbId":81189,"year":2008,"listId":3}]`)
	items, err := parseArrImportListSeries(body, "sonarr_importlist", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MediaType != "tv" || items[0].TMDBID != 1396 {
		t.Fatalf("unexpected: %+v", items)
	}
}

func TestArrImportListProxySource(t *testing.T) {
	parent := &listsyncv1.ListSource{BaseUrl: "http://parent", ApiKey: "parent-key"}
	cfg := arrImportListConfig{
		Name:           "Trakt List",
		Implementation: "TraktListImport",
		Fields: []arrImportListField{
			{Name: "accessToken", Value: "token"},
			{Name: "listId", Value: "user/list-slug"},
		},
	}
	proxy := arrImportListProxySource(parent, cfg)
	if proxy.GetApiKey() != "token" {
		t.Fatalf("api key: %s", proxy.GetApiKey())
	}
	if proxy.GetListUrl() != "user/list-slug" {
		t.Fatalf("list url: %s", proxy.GetListUrl())
	}
}

func TestArrImportListIDFilter(t *testing.T) {
	id, ok := arrImportListIDFilter("list:7")
	if !ok || id != 7 {
		t.Fatalf("expected list 7, got %d ok=%v", id, ok)
	}
}
