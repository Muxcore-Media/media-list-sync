package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	musicv1 "github.com/Muxcore-Media/media-music/proto/gen/muxcore/music/v1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

const (
	cleanLibraryKeep            = "keep"
	cleanLibraryUnmonitor       = "unmonitor"
	cleanLibraryRemove          = "remove"
	cleanLibraryRemoveAndDelete = "remove_and_delete"
)

func normalizeCleanLibraryLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case cleanLibraryUnmonitor, cleanLibraryRemove, cleanLibraryRemoveAndDelete:
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return cleanLibraryKeep
	}
}

type trackedSyncItem struct {
	ExternalID    string
	MediaType     string
	MatchedItemID string
	Status        string
}

func (m *Module) reconcileRemovedItems(ctx context.Context, source *listsyncv1.ListSource, current []syncItem) int {
	present := make(map[string]struct{}, len(current))
	for _, item := range current {
		if item.ExternalID == "" {
			continue
		}
		present[item.ExternalID] = struct{}{}
	}

	level := normalizeCleanLibraryLevel(source.GetCleanLibraryLevel())
	stale, err := m.listStaleSyncItems(ctx, source.GetId(), present)
	if err != nil {
		slog.Warn("list stale sync items failed", "source", source.GetName(), "error", err)
		return 0
	}

	removed := 0
	for _, item := range stale {
		if item.Status == "added" && item.MatchedItemID != "" && level != cleanLibraryKeep {
			if err := m.applyCleanLibraryLevel(ctx, source, item, level); err != nil {
				slog.Warn("clean library failed",
					"source", source.GetName(),
					"external_id", item.ExternalID,
					"level", level,
					"error", err,
				)
			}
		}
		if err := m.markSyncItemRemoved(ctx, source.GetId(), item.ExternalID); err != nil {
			slog.Warn("mark sync item removed failed", "external_id", item.ExternalID, "error", err)
			continue
		}
		removed++
	}
	return removed
}

func (m *Module) listStaleSyncItems(ctx context.Context, sourceID string, present map[string]struct{}) ([]trackedSyncItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return nil, fmt.Errorf("db not initialized")
	}

	rows, err := m.db.QueryContext(ctx, `SELECT external_id, media_type, matched_item_id, status FROM sync_items WHERE source_id = ? AND status != 'removed'`, sourceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var stale []trackedSyncItem
	for rows.Next() {
		var item trackedSyncItem
		if err := rows.Scan(&item.ExternalID, &item.MediaType, &item.MatchedItemID, &item.Status); err != nil {
			continue
		}
		if _, ok := present[item.ExternalID]; ok {
			continue
		}
		stale = append(stale, item)
	}
	return stale, rows.Err()
}

func (m *Module) markSyncItemRemoved(ctx context.Context, sourceID, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return fmt.Errorf("db not initialized")
	}
	now := timeNowRFC3339()
	_, err := m.db.ExecContext(ctx,
		`UPDATE sync_items SET status = 'removed', matched_item_id = '', updated_at = ? WHERE source_id = ? AND external_id = ?`,
		now, sourceID, externalID,
	)
	return err
}

func (m *Module) applyCleanLibraryLevel(ctx context.Context, source *listsyncv1.ListSource, item trackedSyncItem, level string) error {
	if m.cleanLibraryItemFn != nil {
		return m.cleanLibraryItemFn(source, item, level)
	}

	switch level {
	case cleanLibraryUnmonitor:
		return m.unmonitorLibraryItem(ctx, item)
	case cleanLibraryRemove:
		return m.removeLibraryItem(ctx, item, false)
	case cleanLibraryRemoveAndDelete:
		return m.removeLibraryItem(ctx, item, true)
	default:
		return nil
	}
}

func (m *Module) unmonitorLibraryItem(ctx context.Context, item trackedSyncItem) error {
	mon := false
	switch item.MediaType {
	case "movie":
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.UpdateMovie(ctx, &mgmntv1.UpdateMovieRequest{MovieId: item.MatchedItemID, Monitored: &mon})
		return err
	case "tv":
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.UpdateTVShow(ctx, &tvmgmtv1.UpdateTVShowRequest{SeriesId: item.MatchedItemID, Monitored: &mon})
		return err
	case "music":
		return m.setArtistMonitored(ctx, item.MatchedItemID, false)
	default:
		return fmt.Errorf("unsupported media type %q", item.MediaType)
	}
}

func (m *Module) removeLibraryItem(ctx context.Context, item trackedSyncItem, deleteFiles bool) error {
	switch item.MediaType {
	case "movie":
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.RemoveMovie(ctx, &mgmntv1.RemoveMovieRequest{MovieId: item.MatchedItemID, DeleteFiles: deleteFiles})
		return err
	case "tv":
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.RemoveTVShow(ctx, &tvmgmtv1.RemoveTVShowRequest{SeriesId: item.MatchedItemID, DeleteFiles: deleteFiles})
		return err
	case "music":
		if err := m.ensureMusic(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.musicClient
		m.mu.RUnlock()
		_, err := mc.RemoveArtist(ctx, &musicv1.RemoveArtistRequest{Id: item.MatchedItemID, DeleteFiles: deleteFiles})
		return err
	default:
		return fmt.Errorf("unsupported media type %q", item.MediaType)
	}
}

func timeNowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
