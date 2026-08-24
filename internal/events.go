package internal

import (
	"context"
	"encoding/json"
	"log/slog"

	mediacontracts "github.com/Muxcore-Media/contracts-media/events"
	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) publishImportListSynced(source *listsyncv1.ListSource, found, newItems, removed int) {
	if m.mc == nil {
		return
	}
	payload, err := json.Marshal(mediacontracts.ImportListSyncedPayload{
		SourceID:     source.GetId(),
		SourceName:   source.GetName(),
		SourceType:   source.GetType(),
		ItemsFound:   int32(found),
		ItemsNew:     int32(newItems),
		ItemsRemoved: int32(removed),
	})
	if err != nil {
		slog.Warn("marshal importlist.synced payload", "error", err)
		return
	}
	if err := m.mc.Events.Publish(context.Background(), mediacontracts.EventImportListSynced, m.id, payload); err != nil {
		slog.Warn("publish importlist.synced failed", "source", source.GetName(), "error", err)
	}
}
