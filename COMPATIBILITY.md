# Compatibility

## Core Version

| Module Version | Core Version | Status |
|----------------|-------------|--------|
| v0.2.4           | 0.4.0+     | Current |

MVP host stacks pin **core@v0.5.8**. This module declares `minCoreVersion` **0.4.0**.

## Capabilities

- `media.listsync`, `settings`
- `importlist.trakt`, `trakt_list`, `imdb`, `plex`, `jellyfin`, `radarr`, `sonarr`
- `importlist.radarr_importlist`, `sonarr_importlist`
- `importlist.stevenlu`, `tmdb_list`, `rss`, `watcharr`, `lastfm`

## Contracts

No external contract package — list-sync gRPC API for import sources.

**API:** `ListSources`, `AddSource`, `UpdateSource`, `RemoveSource`, `TestSource`, `SyncNow` (optional `source_id`), `GetHistory`, `GetItems` (includes `musicbrainz_id` for music items; `matched_only` filters library-added rows).

**Per-source import policy:** `tag_ids`, `monitor_mode`, `minimum_availability`, `clean_library_level`, `search_on_add`; maintainer `CheckImportBlocked` on movie/TV import; music uses `media-music` with the same monitor/tag/clean-library semantics.

## Breaking Changes

Pre-1.0 module: interfaces may change without a major version bump. Prefer published tags over `main`/`master` HEAD in production.
