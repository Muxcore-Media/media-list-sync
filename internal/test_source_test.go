package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestArrSystemStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/system/status":
			_, _ = w.Write([]byte(`{"version":"5.0.0"}`))
		case "/api/v3/movie":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m := NewModule(Config{})
	msg, count, err := m.testRadarr(context.Background(), &listsyncv1.ListSource{
		BaseUrl: srv.URL,
		ApiKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 movies in empty stub, got %d", count)
	}
	if msg == "" {
		t.Fatal("expected message")
	}
}

func TestArrSystemStatusUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	m := NewModule(Config{})
	_, err := m.arrSystemStatus(context.Background(), srv.URL, "bad")
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
}
