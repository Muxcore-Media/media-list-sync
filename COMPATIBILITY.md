# Compatibility

## Core Version

| Module Version | Core Version | Status |
|----------------|-------------|--------|
| v0.1.1           | 0.4.0+     | Current |

MVP host stacks pin **core@v0.5.0**. This module declares `minCoreVersion` **0.4.0**.

## Capabilities

- `media.listsync`
- `importlist.trakt` / `imdb` / `plex` / `jellyfin` / `radarr` / `sonarr`

## Contracts

No external contract package — list-sync gRPC API for import sources.

**API (v0.1.8+):** `ListSources`, `AddSource`, `UpdateSource`, `RemoveSource`, `TestSource`, `SyncNow` (optional `source_id`), `GetHistory`, `GetItems`.

**v0.2.0+:** per-source `tag_ids`, `monitor_mode`, `minimum_availability`, maintainer `CheckImportBlocked` on import.

## Breaking Changes

Pre-1.0 module: interfaces may change without a major version bump. Prefer published tags over `main`/`master` HEAD in production.
