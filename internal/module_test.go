package internal

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func newTestModule(t *testing.T) *Module {
	t.Helper()
	m := NewModule(Config{
		DBPath:   filepath.Join(t.TempDir(), "listsync.db"),
		GRPCAddr: ":0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { m.Stop(ctx) })
	return m
}

func TestModuleInfo(t *testing.T) {
	m := NewModule(Config{})
	info := m.Info()
	if info.ID == "" {
		t.Error("module ID must not be empty")
	}
	if info.Version == "" {
		t.Error("module version must not be empty")
	}
	found := false
	for _, c := range info.Capabilities {
		if c == "media.listsync" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected media.listsync in %v", info.Capabilities)
	}
	hasImportlist := false
	for _, r := range info.Roles {
		if r == "importlist" {
			hasImportlist = true
		}
	}
	if !hasImportlist {
		t.Errorf("expected importlist role, got %v", info.Roles)
	}
}

func TestListSourcesEmpty(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	sources, err := m.ListSources(ctx, &listsyncv1.ListSourcesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sources.Sources))
	}
}

func TestAddTraktSource(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	added, err := m.AddSource(ctx, &listsyncv1.AddSourceRequest{
		Name:     "My Trakt",
		Type:     "trakt",
		Username: "myuser",
		ClientId: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Source.Name != "My Trakt" {
		t.Errorf("expected My Trakt, got %s", added.Source.Name)
	}
	if added.Source.Type != "trakt" {
		t.Errorf("expected trakt type, got %s", added.Source.Type)
	}
	if added.Source.ClientId != "abc123" {
		t.Errorf("expected abc123, got %s", added.Source.ClientId)
	}

	sources, _ := m.ListSources(ctx, &listsyncv1.ListSourcesRequest{})
	if len(sources.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources.Sources))
	}
}

func TestAddIMDbSource(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	added, err := m.AddSource(ctx, &listsyncv1.AddSourceRequest{
		Name:    "IMDb Watchlist",
		Type:    "imdb",
		ListUrl: "https://www.imdb.com/list/ls123456/export",
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Source.ListUrl != "https://www.imdb.com/list/ls123456/export" {
		t.Errorf("wrong list URL")
	}
}

func TestRemoveSource(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	added, _ := m.AddSource(ctx, &listsyncv1.AddSourceRequest{Name: "Test", Type: "trakt", ClientId: "x"})
	_, err := m.RemoveSource(ctx, &listsyncv1.RemoveSourceRequest{Id: added.Source.Id})
	if err != nil {
		t.Fatal(err)
	}

	sources, _ := m.ListSources(ctx, &listsyncv1.ListSourcesRequest{})
	if len(sources.Sources) != 0 {
		t.Errorf("expected 0 after remove, got %d", len(sources.Sources))
	}
}

func TestGetItemsEmpty(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	items, err := m.GetItems(ctx, &listsyncv1.GetItemsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if items.Total != 0 {
		t.Errorf("expected 0 items, got %d", items.Total)
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	hist, err := m.GetHistory(ctx, &listsyncv1.GetHistoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if hist.Total != 0 {
		t.Errorf("expected 0 history entries, got %d", hist.Total)
	}
}

func TestSyncNow(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	resp, err := m.SyncNow(ctx, &listsyncv1.SyncNowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

func TestStoreItems(t *testing.T) {
	m := newTestModule(t)

	newCount := m.storeItems("test-source", []syncItem{
		{ExternalID: "tt0137523", IMDBID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999, Action: "watchlist"},
		{ExternalID: "tt0903747", IMDBID: "tt0903747", TMDBID: 1668, MediaType: "tv", Title: "Breaking Bad", Year: 2008, Action: "watchlist"},
	})
	if newCount != 2 {
		t.Errorf("expected 2 new items, got %d", newCount)
	}

	// Duplicate should be ignored
	newCount2 := m.storeItems("test-source", []syncItem{
		{ExternalID: "tt0137523", IMDBID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999, Action: "watchlist"},
	})
	if newCount2 != 0 {
		t.Errorf("expected 0 new (duplicate), got %d", newCount2)
	}

	// Verify items in DB
	ctx := context.Background()
	items, _ := m.GetItems(ctx, &listsyncv1.GetItemsRequest{})
	if items.Total != 2 {
		t.Errorf("expected 2 total items, got %d", items.Total)
	}
}

func TestParseIMDbCSV(t *testing.T) {
	csv := `position,const,created,modified,title,titleType,url,imdbid,year,...
1,tt0137523,2024-01-01,2024-01-01,Fight Club,movie,/title/tt0137523/,tt0137523,1999,...
2,tt0903747,2024-01-02,2024-01-02,Breaking Bad,tvSeries,/title/tt0903747/,tt0903747,2008,...`

	items, err := parseIMDbCSV(csv)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
	if items[0].Title != "Fight Club" {
		t.Errorf("expected Fight Club, got %s", items[0].Title)
	}
	if items[0].Year != 1999 {
		t.Errorf("expected year 1999, got %d", items[0].Year)
	}
	if items[0].MediaType != "movie" {
		t.Errorf("expected movie, got %s", items[0].MediaType)
	}
	if items[1].MediaType != "tv" {
		t.Errorf("expected tv for tvSeries, got %s", items[1].MediaType)
	}
}

func TestSplitCSVLine(t *testing.T) {
	line := `1,tt0137523,"Fight Club",movie,2024`
	fields := splitCSVLine(line)
	if len(fields) != 5 {
		t.Errorf("expected 5 fields, got %d: %v", len(fields), fields)
	}
	if fields[0] != "1" || fields[1] != "tt0137523" {
		t.Errorf("unexpected fields: %v", fields)
	}
}

func TestDefaultSyncInterval(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()

	added, _ := m.AddSource(ctx, &listsyncv1.AddSourceRequest{
		Name: "Default", Type: "trakt", ClientId: "x",
	})
	if added.Source.SyncIntervalMinutes != 1440 {
		t.Errorf("expected default 1440 min, got %d", added.Source.SyncIntervalMinutes)
	}
}

func TestLifecycle(t *testing.T) {
	m := NewModule(Config{
		DBPath:   filepath.Join(t.TempDir(), "lifecycle.db"),
		GRPCAddr: ":0",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestHealth(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()
	if err := m.Health(ctx); err != nil {
		t.Fatal("expected health to pass")
	}
}

func TestMarkSyncItemAdded(t *testing.T) {
	m := newTestModule(t)
	sourceID := "src1"
	items := []syncItem{{
		ExternalID: "trakt_1",
		TMDBID:     550,
		MediaType:  "movie",
		Title:      "Fight Club",
		Year:       1999,
		Action:     "watchlist",
	}}
	if n := m.storeItems(sourceID, items); n != 1 {
		t.Fatalf("expected 1 new item, got %d", n)
	}
	m.markSyncItemAdded(sourceID, "trakt_1", "mv_550")

	m.mu.RLock()
	var status, matched string
	err := m.db.QueryRow(
		`SELECT status, matched_item_id FROM sync_items WHERE source_id = ? AND external_id = ?`,
		sourceID, "trakt_1",
	).Scan(&status, &matched)
	m.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if status != "added" {
		t.Errorf("expected status added, got %s", status)
	}
	if matched != "mv_550" {
		t.Errorf("expected matched_item_id mv_550, got %s", matched)
	}
}

func TestAddItemsToLibraryFallbackNoTMDB(t *testing.T) {
	m := newTestModule(t)
	src := &listsyncv1.ListSource{Id: "src"}
	// No mesh client / library modules: items with TMDB fail add and fall back;
	// items without TMDB go straight to automation fallback (also no-op without mesh).
	m.addItemsToLibrary(src, []syncItem{
		{ExternalID: "x1", TMDBID: 0, MediaType: "movie", Title: "Unknown"},
		{ExternalID: "x2", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999},
	})
}

func TestAddItemsToLibrarySkipsAlreadyAdded(t *testing.T) {
	m := newTestModule(t)
	src := &listsyncv1.ListSource{Id: "src1"}
	m.storeItems(src.Id, []syncItem{{
		ExternalID: "trakt_1", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999, Action: "watchlist",
	}})
	m.markSyncItemAdded(src.Id, "trakt_1", "mv_550")
	m.addItemsToLibrary(src, []syncItem{{
		ExternalID: "trakt_1", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999,
	}})
}
