package internal

import (
	"context"
	"encoding/json"
	"log/slog"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

const eventImportListSynced = "importlist.synced"

type importListSyncedPayload struct {
	SourceID     string `json:"source_id"`
	SourceName   string `json:"source_name"`
	SourceType   string `json:"source_type"`
	ItemsFound   int32  `json:"items_found"`
	ItemsNew     int32  `json:"items_new"`
	ItemsRemoved int32  `json:"items_removed"`
}

func (m *Module) publishImportListSynced(source *listsyncv1.ListSource, found, newItems, removed int) {
	if m.mc == nil {
		return
	}
	payload, err := json.Marshal(importListSyncedPayload{
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
	if err := m.mc.Events.Publish(context.Background(), eventImportListSynced, m.id, payload); err != nil {
		slog.Warn("publish importlist.synced failed", "source", source.GetName(), "error", err)
	}
}
