package internal

import (
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
}
