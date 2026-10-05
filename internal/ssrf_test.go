package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestAddSourceRejectsUnsafeBaseURL(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254", "http://metadata.google.internal", "file:///etc/passwd",
		"gopher://x", "http://[fe80::1]:7878", "http://0x7f000001:7878", "ftp://radarr",
	} {
		m := newTestModule(t)
		_, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
			Name: "x", Type: "radarr", BaseUrl: u, ApiKey: "k",
		})
		if err == nil {
			t.Errorf("base_url %q accepted", u)
		}
	}
}

func TestAddSourceAllowsLANBaseURL(t *testing.T) {
	for _, u := range []string{"http://192.168.1.50:7878", "http://localhost:8096", "http://127.0.0.1:7878", "http://radarr:7878", "https://jellyfin.lan"} {
		m := newTestModule(t)
		if _, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
			Name: "x", Type: "radarr", BaseUrl: u, ApiKey: "k",
		}); err != nil {
			t.Errorf("LAN base_url %q rejected: %v", u, err)
		}
	}
}

func TestUpdateSourceRejectsUnsafeBaseURL(t *testing.T) {
	m := newTestModule(t)
	added, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{Name: "x", Type: "sonarr", BaseUrl: "http://sonarr:8989", ApiKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	bad := "http://169.254.169.254"
	if _, err := m.UpdateSource(context.Background(), &listsyncv1.UpdateSourceRequest{Id: added.Source.Id, BaseUrl: &bad}); err == nil {
		t.Fatal("update to metadata base_url accepted")
	}
}

func TestFetchArrJellyfinBlockMetadataBaseURL(t *testing.T) {
	m := newTestModule(t)
	src := &listsyncv1.ListSource{Name: "s", BaseUrl: "http://169.254.169.254", ApiKey: "k", Username: "u"}
	if _, err := m.fetchRadarr(src); err == nil {
		t.Error("radarr fetch to metadata allowed")
	}
	if _, err := m.fetchSonarr(src); err == nil {
		t.Error("sonarr fetch to metadata allowed")
	}
	if _, err := m.fetchJellyfin(src); err == nil {
		t.Error("jellyfin fetch to metadata allowed")
	}
}

func TestFetchRadarrLoopbackServerWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"title":"A","year":2020,"tmdbId":5,"monitored":true}]`))
	}))
	defer srv.Close()
	m := newTestModule(t)
	items, err := m.fetchRadarr(&listsyncv1.ListSource{Name: "s", BaseUrl: srv.URL, ApiKey: "k"})
	if err != nil || len(items) != 1 {
		t.Fatalf("local radarr: %v %v", items, err)
	}
}

func TestIMDbListURLPinnedToIMDb(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()
	m := newTestModule(t)
	for _, u := range []string{
		srv.URL + "/export", "https://evil.example.com/export", "http://www.imdb.com/list/ls1/export",
		"https://www.imdb.com.evil.example.com/list/ls1/export", "https://169.254.169.254/export",
		"https://127.0.0.1/list/export",
	} {
		_, err := m.fetchIMDb(&listsyncv1.ListSource{Name: "s", ListUrl: u})
		if err == nil || !strings.Contains(err.Error(), "imdb list_url") {
			t.Errorf("fetchIMDb(%q) = %v", u, err)
		}
		m2 := newTestModule(t)
		if _, err := m2.AddSource(context.Background(), &listsyncv1.AddSourceRequest{Name: "i", Type: "imdb", ListUrl: u}); err == nil {
			t.Errorf("AddSource imdb accepted %q", u)
		}
	}
	if hit {
		t.Fatal("request reached test server")
	}
}
