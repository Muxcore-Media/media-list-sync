package internal

import (
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
	if n := m.storeItems(src.Id, items); n != 1 {
		t.Fatalf("store=%d", n)
	}
	m.markSyncItemAdded(src.Id, "tt0137523", "mv1")

	removed := m.reconcileRemovedItems(src, nil)
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if status := m.syncItemStatus(src.Id, "tt0137523"); status != "removed" {
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
	m.storeItems(src.Id, items)
	m.markSyncItemAdded(src.Id, "tt0137523", "mv1")

	if n := m.reconcileRemovedItems(src, nil); n != 1 {
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
	m.storeItems(srcID, items)
	m.markSyncItemAdded(srcID, "tt0137523", "mv1")
	_ = m.markSyncItemRemoved(srcID, "tt0137523")

	if n := m.storeItems(srcID, items); n != 0 {
		t.Fatalf("expected 0 new on re-add, got %d", n)
	}
	if status := m.syncItemStatus(srcID, "tt0137523"); status != "pending" {
		t.Fatalf("status=%q", status)
	}
}
