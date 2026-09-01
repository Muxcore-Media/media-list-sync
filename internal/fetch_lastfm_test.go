package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestParseLastfmArtists(t *testing.T) {
	body := []byte(`{"topartists":{"artist":[{"name":"Radiohead","mbid":"a74b1b7f-71a5-396a-9c10-038d927792b6"}]}}`)
	items, err := parseLastfmArtists(body, "lastfm_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].MediaType != "music" || items[0].Title != "Radiohead" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
	if items[0].MusicBrainzID == "" {
		t.Fatal("expected musicbrainz id")
	}
}

func TestParseLastfmLovedTracks(t *testing.T) {
	body := []byte(`{"lovedtracks":{"track":[
		{"name":"Paranoid Android","artist":{"name":"Radiohead","mbid":"a74b1b7f-71a5-396a-9c10-038d927792b6"}},
		{"name":"Karma Police","artist":{"name":"Radiohead","mbid":"a74b1b7f-71a5-396a-9c10-038d927792b6"}}
	]}}`)
	items, err := parseLastfmLovedTracks(body, "ender")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected deduped artist, got %d", len(items))
	}
}

func TestParseLastfmListSpec(t *testing.T) {
	spec := parseLastfmListSpec(&listsyncv1.ListSource{ListUrl: "tag:rock"})
	if spec.kind != "tag" || spec.tag != "rock" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	spec = parseLastfmListSpec(&listsyncv1.ListSource{ListUrl: "user:ender period:1month limit:25"})
	if spec.user != "ender" || spec.period != "1month" || spec.limit != 25 {
		t.Fatalf("combined spec=%+v", spec)
	}
}

func TestFetchLastfmChartViaHTTPTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") != "chart.gettopartists" {
			t.Fatalf("method=%s", r.URL.Query().Get("method"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artists": map[string]any{
				"artist": []map[string]string{{"name": "Artist A", "mbid": "mbid-a"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	m := newTestModule(t)
	m.lastfmAPIBase = srv.URL
	source := &listsyncv1.ListSource{ApiKey: "key", ListUrl: "chart limit:1"}
	items, err := m.fetchLastfm(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Artist A" {
		t.Fatalf("items=%+v", items)
	}
}

func TestFetchLastfmTagLovedLibraryTop(t *testing.T) {
	methods := map[string]string{
		"tag:rock":               "tag.gettopartists",
		"loved:ender":            "user.getlovedtracks",
		"library:ender":          "user.getartists",
		"user:ender period:7day": "user.gettopartists",
	}
	for listURL, wantMethod := range methods {
		t.Run(listURL, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("method"); got != wantMethod {
					t.Fatalf("method=%s want=%s", got, wantMethod)
				}
				switch wantMethod {
				case "user.getlovedtracks":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"lovedtracks": map[string]any{
							"track": []map[string]any{{
								"name":   "Song",
								"artist": map[string]string{"name": "Radiohead", "mbid": "mbid-1"},
							}},
						},
					})
				default:
					_ = json.NewEncoder(w).Encode(map[string]any{
						"topartists": map[string]any{
							"artist": []map[string]string{{"name": "Radiohead", "mbid": "mbid-1"}},
						},
					})
				}
			}))
			t.Cleanup(srv.Close)

			m := newTestModule(t)
			m.lastfmAPIBase = srv.URL
			items, err := m.fetchLastfm(context.Background(), &listsyncv1.ListSource{ApiKey: "key", ListUrl: listURL})
			if err != nil {
				t.Fatal(err)
			}
			if len(items) == 0 {
				t.Fatal("expected items")
			}
		})
	}
}

func TestFetchTraktPagination(t *testing.T) {
	moviePage := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Pagination-Page-Count", "2")
		switch {
		case strings.Contains(r.URL.Path, "/movies"):
			moviePage++
			if moviePage == 1 {
				_ = json.NewEncoder(w).Encode([]traktWatchlistMovie{{
					Movie: traktMovie{Title: "A", IDs: traktIDs{Trakt: 1, TMDB: 10}},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode([]traktWatchlistMovie{{
				Movie: traktMovie{Title: "B", IDs: traktIDs{Trakt: 2, TMDB: 20}},
			}})
		default:
			_ = json.NewEncoder(w).Encode([]traktWatchlistShow{})
		}
	}))
	t.Cleanup(srv.Close)

	m := newTestModule(t)
	m.traktAPIBase = srv.URL
	items, err := m.fetchTrakt(context.Background(), &listsyncv1.ListSource{
		Name: "Trakt", ClientId: "cid", ApiKey: "token", ListUrl: "watchlist",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items across pages, got %d", len(items))
	}
}

func TestFetchTraktCollectionKind(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("X-Pagination-Page-Count", "1")
		if strings.Contains(gotPath, "movies") {
			_ = json.NewEncoder(w).Encode([]traktCollectionMovie{{
				Movie: traktMovie{Title: "Movie", IDs: traktIDs{Trakt: 3, TMDB: 30}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode([]traktCollectionShow{})
	}))
	t.Cleanup(srv.Close)

	m := newTestModule(t)
	m.traktAPIBase = srv.URL
	items, err := m.fetchTrakt(context.Background(), &listsyncv1.ListSource{
		ClientId: "cid", ApiKey: "token", ListUrl: "collection",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != "collection" {
		t.Fatalf("items=%+v path=%s", items, gotPath)
	}
}
