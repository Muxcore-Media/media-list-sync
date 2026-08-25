package internal

import (
	"testing"
)

func TestParseWatcharrListURL(t *testing.T) {
	mode, arg := parseWatcharrListURL("status:PLANNED,WATCHING")
	if mode != "status" || arg != "PLANNED,WATCHING" {
		t.Fatalf("unexpected status parse: %q %q", mode, arg)
	}
	mode, arg = parseWatcharrListURL("tag:12")
	if mode != "tag" || arg != "12" {
		t.Fatalf("unexpected tag parse: %q %q", mode, arg)
	}
	mode, arg = parseWatcharrListURL("public:5/alice")
	if mode != "public" || arg != "5/alice" {
		t.Fatalf("unexpected public parse: %q %q", mode, arg)
	}
}

func TestWatcharrMediaToSyncItem(t *testing.T) {
	item, ok := watcharrMediaToSyncItem(watcharrMedia{
		Type: "tmdb_movie",
		Name: "Fight Club",
		IDs: struct {
			TMDB int    `json:"tmdb"`
			IMDB string `json:"imdb"`
		}{TMDB: 550, IMDB: "tt0137523"},
		ReleaseDate: "1999-10-15",
		Watched: struct {
			ID uint `json:"id"`
		}{ID: 42},
	})
	if !ok {
		t.Fatal("expected ok")
	}
	if item.MediaType != "movie" || item.TMDBID != 550 || item.ExternalID != "watcharr_42" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestParseWatcharrMediaListArray(t *testing.T) {
	body := []byte(`[
		{"type":"tmdb_tv","name":"Breaking Bad","ids":{"tmdb":1396},"releaseDate":"2008-01-20","watched":{"id":7}}
	]`)
	items, err := parseWatcharrMediaList(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MediaType != "tv" {
		t.Fatalf("unexpected: %+v", items)
	}
}

func TestParseWatcharrPagedMedia(t *testing.T) {
	body := []byte(`{"results":[{"type":"tmdb_movie","name":"Alien","ids":{"tmdb":348},"watched":{"id":1}}],"totalPages":2}`)
	items, totalPages, err := parseWatcharrPagedMedia(body)
	if err != nil {
		t.Fatal(err)
	}
	if totalPages != 2 || len(items) != 1 {
		t.Fatalf("unexpected page: pages=%d len=%d", totalPages, len(items))
	}
}
