package internal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestTriggerArrImportListSync(t *testing.T) {
	var commandGET int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"name":"ImportListSync","status":"started"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/command/42":
			commandGET++
			w.Header().Set("Content-Type", "application/json")
			status := "started"
			if commandGET >= 2 {
				status = "completed"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{})
	m.httpCli = srv.Client()
	source := &listsyncv1.ListSource{BaseUrl: srv.URL, ApiKey: "test-key"}
	if err := m.triggerArrImportListSync(source); err != nil {
		t.Fatal(err)
	}
	if commandGET < 2 {
		t.Fatalf("expected command polling, got %d polls", commandGET)
	}
}
