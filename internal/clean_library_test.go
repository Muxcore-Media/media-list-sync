package internal

import (
	"context"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestNormalizeCleanLibraryLevel(t *testing.T) {
	if got := normalizeCleanLibraryLevel(""); got != cleanLibraryKeep {
		t.Fatalf("empty = %q", got)
	}
	if got := normalizeCleanLibraryLevel("UNMONITOR"); got != cleanLibraryUnmonitor {
		t.Fatalf("unmonitor = %q", got)
	}
	if got := normalizeCleanLibraryLevel("bogus"); got != cleanLibraryKeep {
		t.Fatalf("bogus = %q", got)
	}
}

func TestReconcileRemovedItemsKeep(t *testing.T) {
	m := newTestModule(t)
	src := &listsyncv1.ListSource{Id: "src1", Name: "Test", CleanLibraryLevel: cleanLibraryKeep}
	items := []syncItem{
		{ExternalID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999},
	}
	if n := m.storeItems(context.Background(), src.Id, items); n != 1 {
		t.Fatalf("store=%d", n)
	}
	m.markSyncItemAdded(context.Background(), src.Id, "tt0137523", "mv1")

	removed := m.reconcileRemovedItems(context.Background(), src, nil)
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if status := m.syncItemStatus(context.Background(), src.Id, "tt0137523"); status != "removed" {
		t.Fatalf("status=%q", status)
	}
}

func TestReconcileRemovedItemsCallsCleanHook(t *testing.T) {
	m := newTestModule(t)
	called := false
	m.cleanLibraryItemFn = func(source *listsyncv1.ListSource, item trackedSyncItem, level string) error {
		called = true
		if level != cleanLibraryUnmonitor {
			t.Fatalf("level=%q", level)
		}
		if item.MatchedItemID != "mv1" {
			t.Fatalf("matched=%q", item.MatchedItemID)
		}
		return nil
	}

	src := &listsyncv1.ListSource{Id: "src1", Name: "Test", CleanLibraryLevel: cleanLibraryUnmonitor}
	items := []syncItem{{ExternalID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999}}
	m.storeItems(context.Background(), src.Id, items)
	m.markSyncItemAdded(context.Background(), src.Id, "tt0137523", "mv1")

	if n := m.reconcileRemovedItems(context.Background(), src, nil); n != 1 {
		t.Fatalf("removed=%d", n)
	}
	if !called {
		t.Fatal("expected clean hook")
	}
}

func TestStoreItemsResetsRemovedToPending(t *testing.T) {
	m := newTestModule(t)
	srcID := "src_reset"
	items := []syncItem{{ExternalID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999}}
	m.storeItems(context.Background(), srcID, items)
	m.markSyncItemAdded(context.Background(), srcID, "tt0137523", "mv1")
	_ = m.markSyncItemRemoved(context.Background(), srcID, "tt0137523")

	if n := m.storeItems(context.Background(), srcID, items); n != 0 {
		t.Fatalf("expected 0 new on re-add, got %d", n)
	}
	if status := m.syncItemStatus(context.Background(), srcID, "tt0137523"); status != "pending" {
		t.Fatalf("status=%q", status)
	}
}
