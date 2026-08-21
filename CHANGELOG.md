# Changelog

## [0.2.3] — 2026-08-20

### Added

- **`watcharr`** list source — sync movies/TV from a Watcharr instance via `/api/watched`, tag lists, public lists, or status filters (`status:PLANNED`, `tag:<id>`, `public:<userId>/<username>`).
- JWT auth via `api_key`, or username + password login per sync.

## [0.2.2] — 2026-08-20

### Added

- **`radarr_importlist`** source — sync pending movies from external Radarr via `GET /api/v3/importlist/movie` (optional `list_url` = `list:<id>`).
- **`sonarr_importlist`** source — sync via `GET /api/v3/importlist/series`, with fallback that reads `/api/v3/importlist` configs and proxies known list providers.
- Richer **RSS** title parsing for `{imdb:tt…}` / `{tmdb:123}` tags (Radarr-style custom feeds).
- **`radarr_importlist` / `sonarr_importlist`** trigger remote `ImportListSync` before fetch (best-effort; polls command completion).

## [0.2.1] — 2026-08-20

### Added

- Per-source **`search_on_add`** toggle (TV missing-episode queue + movie automation search).
- **`rss`** list provider (IMDb/TMDB IDs from feed items).
- **TMDB list presets** in `tmdb_list` (`trending/movie/week`, `movie/popular`, `tv/on_the_air`, etc.).
- Admin UI: all list source types, quality profile picker, root folder picker, search-on-add checkbox.

## [0.2.0] — 2026-08-20

### Added

- Per-source **tags on import** (`tag_ids` → `SetItemTags`).
- **Monitor mode** (`all`, `future`, `none`) with TV future-episode season unmonitor.
- **Minimum availability** gate (`announced`, `incinemas`, `released`, `predb`) via metadata-tmdb.
- **List exclusion check** on import via `media-library-maintainer` `CheckImportBlocked`.
- Admin UI fields for tags, monitor mode, and minimum availability.

## [0.1.9] — 2026-08-20

### Added

- Per-source **clean library level** (Radarr/Sonarr parity): `keep`, `unmonitor`, `remove`, `remove_and_delete`.
- Reconcile stale sync items when they leave a list; optional library unmonitor/remove via `media-movies` / `media-tvshows`.
- Emit `importlist.synced` mesh events after successful syncs.
- Sync log / `SyncNow` response include `items_removed`.
- Admin UI clean library level selector; history shows removed count.

## [0.1.8] — 2026-08-20

### Added

- Radarr/Sonarr import-list parity RPCs: `UpdateSource`, `TestSource`, per-source `SyncNow.source_id`.
- `LISTSYNC-PARITY.md` checklist for Radarr/Sonarr Import Lists operator parity.
- Admin UI: edit source, enable/disable, per-source sync, test connection, sync items browser.

## [0.1.7] — 2026-08-10

### Added

- Advertise `settings` capability so admin-ui discovers SettingsProvider without ListAll probing.

## [0.1.6] — 2026-08-10

### Added
- SettingsProvider mesh (`RegisterSettings`) for live `poll_interval` (list sync scanner cadence).

### Changed
- Pin `core/sdk/go/module` to **v0.5.2**.


## [0.1.5] — 2026-08-10

### Fixed
- Sync Info()/muxcore.json version to **0.1.5**.

## v0.1.3 (2026-08-10)

- Pass absolute episode number, series type, series id, and quality profile when queueing missing episodes (media-automation v0.1.3)

## v0.1.2 (2026-08-10)

- After list sync, queue missing monitored episodes for TV series (RefreshMetadata + ListMissing → automation), not only series/movie rows

## v0.1.1 (2026-08-09)

- Sync watchlists/collections from Trakt, IMDb, Plex, Jellyfin, Radarr, Sonarr
- Optional MVP host/compose profile (`list-sync` / `MVP_ENABLE_MEDIA_LIST_SYNC=1`)
