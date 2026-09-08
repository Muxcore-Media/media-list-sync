# Media List Sync

Sync media watchlists and collections from Trakt, IMDb, Plex, Jellyfin, Radarr, and Sonarr into MuxCore.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LISTSYNC_DB_PATH` | `/var/lib/media-list-sync/listsync.db` | SQLite database path |
| `LISTSYNC_GRPC_ADDR` | `:9530` | gRPC listen address |
| `MUXCORE_INSECURE_DISABLE_TLS` | `false` | Disable TLS for module↔core gRPC (`true` for local dev) |

`SyncNow` with an empty request syncs every enabled source. Set `source_id` to sync one list (household Settings per-row Sync and admin-ui `/list-sync/sources/{id}/sync`). `TestSource` fetches that list without importing (household Test and admin-ui `/list-sync/sources/{id}/test`).

## Capability

`media.listsync`, `importlist.trakt`, `importlist.imdb`, `importlist.plex`, `importlist.jellyfin`, `importlist.radarr`, `importlist.sonarr`

## Quick Start

```bash
go build -o media-list-sync ./cmd/module

export MUXCORE_INSECURE_DISABLE_TLS=true
./media-list-sync --muxcore-mesh-addr localhost:9090
```

## License

GPL-3.0
