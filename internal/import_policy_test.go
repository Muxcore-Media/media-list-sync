package internal

import (
	"context"
	"testing"
	"time"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestNormalizeMonitorMode(t *testing.T) {
	if got := normalizeMonitorMode(""); got != monitorModeAll {
		t.Fatalf("got %q", got)
	}
	if got := normalizeMonitorMode("FUTURE"); got != monitorModeFuture {
		t.Fatalf("got %q", got)
	}
}

func TestParseTagIDs(t *testing.T) {
	ids := parseTagIDs(" a, b ,,c ")
	if len(ids) != 3 || ids[0] != "a" || ids[2] != "c" {
		t.Fatalf("got %v", ids)
	}
}

func TestDateOnOrBeforeToday(t *testing.T) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if !dateOnOrBeforeToday(yesterday) {
		t.Fatal("expected yesterday to pass")
	}
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	if dateOnOrBeforeToday(tomorrow) {
		t.Fatal("expected tomorrow to fail")
	}
}

func TestMovieMeetsAvailability(t *testing.T) {
	if !movieMeetsAvailability(minAvailAnnounced, "", "") {
		t.Fatal("announced should pass")
	}
	if movieMeetsAvailability(minAvailReleased, "", "Released") {
		t.Fatal("released without date should fail")
	}
	yesterday := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	if !movieMeetsAvailability(minAvailReleased, yesterday, "Released") {
		t.Fatal("past release should pass")
	}
}

func TestShouldSkipImportBlocked(t *testing.T) {
	m := newTestModule(t)
	m.checkImportBlockedFn = func(_ context.Context, item syncItem) (bool, string) {
		if item.TMDBID == 550 {
			return true, "import exclusion"
		}
		return false, ""
	}
	src := &listsyncv1.ListSource{MinimumAvailability: minAvailAnnounced}
	if skip, reason := m.shouldSkipImport(context.Background(), src, syncItem{TMDBID: 550, MediaType: "movie"}); !skip || reason == "" {
		t.Fatalf("skip=%v reason=%q", skip, reason)
	}
	if skip, _ := m.shouldSkipImport(context.Background(), src, syncItem{TMDBID: 999, MediaType: "movie"}); skip {
		t.Fatal("expected not skipped")
	}
}

func TestMarkSyncItemSkipped(t *testing.T) {
	m := newTestModule(t)
	m.storeItems(context.Background(), "src1", []syncItem{{ExternalID: "x", TMDBID: 1, MediaType: "movie", Title: "X"}})
	m.markSyncItemSkipped(context.Background(), "src1", "x", "import exclusion")
	if status := m.syncItemStatus(context.Background(), "src1", "x"); status != "skipped" {
		t.Fatalf("status=%q", status)
	}
}
