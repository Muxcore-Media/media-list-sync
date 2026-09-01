# Media List Sync

Sync media watchlists and collections from Trakt, IMDb, Plex, Jellyfin, Last.fm, Radarr, and Sonarr into MuxCore.

Radarr/Sonarr **Import Lists** operator parity is tracked in [`LISTSYNC-PARITY.md`](./LISTSYNC-PARITY.md).

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `LISTSYNC_DB_PATH` | `/var/lib/media-list-sync/listsync.db` | SQLite database path |
| `LISTSYNC_GRPC_ADDR` | `:9530` | gRPC listen address |
| `LISTSYNC_POLL_INTERVAL` | `15m` | Background poll interval for due source syncs (Go duration) |
| `MUXCORE_INSECURE_DISABLE_TLS` | `false` | Disable TLS for module↔core gRPC (`true` for local dev) |

## Capabilities

`media.listsync`, `settings`, and import-list providers:

| Capability | Source `type` |
|------------|---------------|
| `importlist.trakt` | `trakt` — `list_url`: `watchlist` (default), `collection`, or `watched` |
| `importlist.trakt_list` | `trakt_list` |
| `importlist.imdb` | `imdb` |
| `importlist.plex` | `plex` |
| `importlist.jellyfin` | `jellyfin` |
| `importlist.radarr` | `radarr` |
| `importlist.sonarr` | `sonarr` |
| `importlist.radarr_importlist` | `radarr_importlist` |
| `importlist.sonarr_importlist` | `sonarr_importlist` |
| `importlist.stevenlu` | `stevenlu` |
| `importlist.tmdb_list` | `tmdb_list` |
| `importlist.rss` | `rss` |
| `importlist.watcharr` | `watcharr` |
| `importlist.lastfm` | `lastfm` |

### Last.fm `list_url` grammar

Space-separated tokens (defaults to the authenticated user's top artists):

| Token | Meaning |
|-------|---------|
| `chart` | Global chart top artists |
| `tag:NAME` | Top artists for tag |
| `loved:USER` | Loved-track artists for user |
| `library:USER` | User library artists |
| `user:NAME` | Top artists for user |
| `period:7day` / `1month` / `overall` | Trakt top-artists period |
| `limit:N` | Max items (default 100, max 1000) |

Example: `user:ender period:1month limit:50`

## Quick Start

```bash
go build -o media-list-sync ./cmd/module

export MUXCORE_INSECURE_DISABLE_TLS=true
./media-list-sync --muxcore-mesh-addr localhost:9090
```

## License

GPL-3.0
