# Radarr / Sonarr Import Lists → MuxCore (`media-list-sync`) parity

**Goal:** `media-list-sync` (+ `admin-ui` `/list-sync`) replaces Radarr and Sonarr **Import Lists** as MuxCore’s consolidated list-ingest layer.

Legend: `[x]` shipped · `[~]` partial · `[ ]` missing · **Waiver** = accepted MuxCore-native equivalent

**Baseline:** `media-list-sync` **v0.2.4** — native providers plus external Arr import-list sync, Watcharr, and Last.fm music lists.

---

## A. Core import-list pipeline

| Radarr / Sonarr Import Lists | Status |
|------------------------------|--------|
| Multiple list sources per instance | [x] SQLite-backed `list_sources` |
| Scheduled sync interval per list | [x] `sync_interval_minutes` + global `poll_interval` setting |
| Manual sync all lists | [x] `SyncNow` + admin **Sync now** |
| Manual sync one list | [x] `SyncNow.source_id` + per-source admin action |
| Sync history / log | [x] `GetHistory` + `/list-sync/history` |
| Add new items from list | [x] Ingest via `media-movies` / `media-tvshows` / `media-music` (Last.fm) |
| TV missing-episode queue after add | [x] `ListMissing` → `media-automation` (when `search_on_add`) |
| TMDB resolution fallback | [x] `metadata-tmdb` search when IDs missing |
| Remove / unmonitor when item leaves list | [x] Per-source `clean_library_level` |
| List exclusions on import | [x] `media-library-maintainer` `CheckImportBlocked` |
| `importlist.synced` mesh events | [x] Published after each successful source sync |
| `contracts-importlist` provider interface | **Waiver** — native `ListSyncService` gRPC |

---

## B. List provider types

| Provider | Radarr / Sonarr | MuxCore `media-list-sync` | Status |
|----------|-----------------|---------------------------|--------|
| Trakt watchlist | Yes | `trakt` | [x] |
| Trakt custom / trending / popular lists | Yes | `trakt_list` | [x] |
| IMDb list / watchlist | Yes | `imdb` | [x] |
| Plex watchlist | Yes | `plex` | [x] |
| Jellyfin favorites / playlist | Yes | `jellyfin` | [x] |
| Watcharr watched / tag / public lists | Yes | `watcharr` | [x] JWT or username/password; `status:`, `tag:`, `public:` filters |
| StevenLu / TMDB list / RSS | Yes | `stevenlu`, `tmdb_list`, `rss` | [x] |
| Last.fm top/chart/tag/loved/library | — | `lastfm` | [x] `user:`, `tag:`, `chart`, `loved:`, `library:`, `period:`, `limit:` |
| External Radarr import lists | Yes | `radarr_importlist` → `GET /api/v3/importlist/movie` | [x] Optional `list_url` = `list:<id>` filter |
| External Sonarr import lists | Yes | `sonarr_importlist` → `GET /api/v3/importlist/series` or config proxy | [x] Falls back to reading `/api/v3/importlist` configs when series API missing |
| External Radarr library mirror | Alternate | `radarr` → monitored `/api/v3/movie` | [x] **Waiver** for full library: `/migrate` |
| External Sonarr library mirror | Alternate | `sonarr` → monitored `/api/v3/series` | [x] same as Radarr |

---

## C. Per-list operator settings (Radarr / Sonarr UI)

| Setting | Status |
|---------|--------|
| Name, type, credentials | [x] |
| Enable / disable list | [x] |
| Quality profile | [x] Profile picker + free-text fallback |
| Root folder | [x] Root picker + free-text fallback |
| Clean library level | [x] |
| Tags on import | [x] |
| Monitor mode | [x] |
| Minimum availability | [x] |
| Search on add | [x] |
| Test connection | [x] |

---

## D. Admin / ops surfaces

All Radarr/Sonarr Import List operator surfaces are covered via gRPC + `/list-sync` admin UI (see v0.1.8 baseline).

---

## E. MuxCore integrations

See v0.2.0 baseline — unchanged.

---

## F. Remaining work

None required for Import List parity.
