# Changelog

## [0.1.12] - 2026-10-05

### Changed
- Built on core v0.6.14 / sdk/go/module v0.6.4: unregisters on shutdown and re-registers after core restarts (ADR-0022).

## [0.1.11] - 2026-10-05


### Changed
- Reported version comes from muxcore.json (ADR-0021); built on core v0.6.12 / sdk/go/module v0.6.3 (mesh enrollment, ADR-0017).

## [0.1.10] - 2026-10-05

### Fixed
- `RemoveSource` returns database errors instead of reporting success.
- Failed sync-log writes are logged instead of silently ignored.

### Changed
- Dependencies resolve from published GitHub tags (core v0.6.2, media-automation v0.1.46); CI on GitHub-hosted runners from the umbrella template.

## [0.1.10] — 2026-09-08

### Added
- `TestSource` fetches one import list without writing sync history or library items (admin-ui Test and household Settings Test).

## [0.1.9] — 2026-09-08

### Added
- `SyncNow.source_id` syncs one import list (admin-ui and household Settings per-row Sync). Empty still syncs every enabled source.

## [0.1.8] — 2026-09-08

### Added
- `UpdateSource` so household Settings can pause or edit an import list without delete-and-re-add.

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
