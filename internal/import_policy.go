package internal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	metadatav1 "github.com/Muxcore-Media/contracts-metadata/muxcore/metadata/v1"
	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	musicv1 "github.com/Muxcore-Media/media-music/proto/gen/muxcore/music/v1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	monitorModeAll    = "all"
	monitorModeFuture = "future"
	monitorModeNone   = "none"

	minAvailAnnounced = "announced"
	minAvailInCinemas = "incinemas"
	minAvailReleased  = "released"
	minAvailPreDB     = "predb"
)

func normalizeMonitorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case monitorModeFuture, monitorModeNone:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return monitorModeAll
	}
}

func normalizeMinimumAvailability(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case minAvailAnnounced, minAvailInCinemas, minAvailReleased, minAvailPreDB:
		return strings.ToLower(strings.TrimSpace(level))
	default:
		return minAvailAnnounced
	}
}

func parseTagIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m *Module) shouldSkipImport(ctx context.Context, source *listsyncv1.ListSource, item syncItem) (bool, string) {
	if blocked, reason := m.checkImportBlocked(ctx, item); blocked {
		return true, reason
	}
	if ok, reason := m.meetsMinimumAvailability(ctx, source, item); !ok {
		return true, reason
	}
	return false, ""
}

func (m *Module) checkImportBlocked(ctx context.Context, item syncItem) (bool, string) {
	if m.checkImportBlockedFn != nil {
		return m.checkImportBlockedFn(ctx, item)
	}
	if item.TMDBID <= 0 {
		return false, ""
	}
	if err := m.ensureMaintainer(ctx); err != nil {
		return false, ""
	}
	scope := "movie"
	if item.MediaType == "tv" {
		scope = "series"
	}
	m.mu.RLock()
	client := m.maintainerClient
	m.mu.RUnlock()
	if client == nil {
		return false, ""
	}
	resp, err := client.CheckImportBlocked(ctx, &maintainv1.CheckImportBlockedRequest{
		Scope:  scope,
		TmdbId: clampInt32FromInt(item.TMDBID),
	})
	if err != nil {
		slog.Debug("maintainer CheckImportBlocked failed", "error", err)
		return false, ""
	}
	if resp.GetBlocked() {
		reason := resp.GetReason()
		if reason == "" {
			reason = "import blocked"
		}
		return true, reason
	}
	return false, ""
}

func (m *Module) meetsMinimumAvailability(ctx context.Context, source *listsyncv1.ListSource, item syncItem) (bool, string) {
	level := normalizeMinimumAvailability(source.GetMinimumAvailability())
	if level == minAvailAnnounced || item.TMDBID <= 0 {
		return true, ""
	}
	if err := m.ensureMetadata(ctx); err != nil {
		return true, ""
	}
	m.mu.RLock()
	mc := m.metadataClient
	m.mu.RUnlock()
	if mc == nil {
		return true, ""
	}

	switch item.MediaType {
	case "movie":
		resp, err := mc.GetMovieDetails(ctx, &metadatav1.GetMovieDetailsRequest{Id: clampInt32FromInt(item.TMDBID)})
		if err != nil {
			return true, ""
		}
		return movieMeetsAvailability(level, resp.GetReleaseDate(), resp.GetStatus()), "minimum availability"
	case "tv":
		resp, err := mc.GetTVDetails(ctx, &metadatav1.GetTVDetailsRequest{Id: clampInt32FromInt(item.TMDBID)})
		if err != nil {
			return true, ""
		}
		return tvMeetsAvailability(level, resp.GetFirstAirDate(), resp.GetStatus()), "minimum availability"
	default:
		return true, ""
	}
}

func movieMeetsAvailability(level, releaseDate, status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch level {
	case minAvailInCinemas:
		if status == "rumored" || status == "planned" {
			return false
		}
		return releaseDate != ""
	case minAvailReleased:
		return dateOnOrBeforeToday(releaseDate)
	default:
		return true
	}
}

func tvMeetsAvailability(level, firstAirDate, status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	switch level {
	case minAvailPreDB:
		return firstAirDate == "" || !dateOnOrBeforeToday(firstAirDate)
	case minAvailInCinemas:
		return firstAirDate != "" || status == "returning series" || status == "ended"
	case minAvailReleased:
		return dateOnOrBeforeToday(firstAirDate)
	default:
		return true
	}
}

func dateOnOrBeforeToday(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !t.After(today)
}

func (m *Module) applyImportPolicy(ctx context.Context, source *listsyncv1.ListSource, item syncItem, libraryID string) {
	if err := m.applyMonitorMode(ctx, source, item, libraryID); err != nil {
		slog.Debug("apply monitor mode failed", "title", item.Title, "error", err)
	}
	if err := m.applyImportTags(ctx, item, libraryID, source.GetTagIds()); err != nil {
		slog.Debug("apply import tags failed", "title", item.Title, "error", err)
	}
}

func (m *Module) applyImportTags(ctx context.Context, item syncItem, libraryID, tagIDsRaw string) error {
	tagIDs := parseTagIDs(tagIDsRaw)
	if len(tagIDs) == 0 || libraryID == "" {
		return nil
	}
	if m.applyImportTagsFn != nil {
		return m.applyImportTagsFn(ctx, item, libraryID, tagIDs)
	}
	switch item.MediaType {
	case "movie":
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.SetItemTags(ctx, &mgmntv1.SetItemTagsRequest{ItemId: libraryID, TagIds: tagIDs})
		return err
	case "tv":
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.SetItemTags(ctx, &tvmgmtv1.SetItemTagsRequest{ItemId: libraryID, TagIds: tagIDs})
		return err
	case "music":
		if err := m.ensureMusic(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.musicClient
		m.mu.RUnlock()
		_, err := mc.SetItemTags(ctx, &musicv1.SetItemTagsRequest{ItemId: libraryID, TagIds: tagIDs})
		return err
	default:
		return nil
	}
}

func (m *Module) applyMonitorMode(ctx context.Context, source *listsyncv1.ListSource, item syncItem, libraryID string) error {
	mode := normalizeMonitorMode(source.GetMonitorMode())
	if mode == monitorModeAll || libraryID == "" {
		return nil
	}
	if m.applyMonitorModeFn != nil {
		return m.applyMonitorModeFn(ctx, source, item, libraryID, mode)
	}
	switch item.MediaType {
	case "movie":
		return m.setMovieMonitored(ctx, libraryID, mode != monitorModeNone)
	case "tv":
		if mode == monitorModeNone {
			return m.setSeriesMonitored(ctx, libraryID, false)
		}
		return m.applyTVFutureMonitor(ctx, libraryID)
	case "music":
		return m.setArtistMonitored(ctx, libraryID, mode != monitorModeNone)
	default:
		return nil
	}
}

func (m *Module) setMovieMonitored(ctx context.Context, movieID string, monitored bool) error {
	if err := m.ensureMovies(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	mc := m.moviesClient
	m.mu.RUnlock()
	_, err := mc.UpdateMovie(ctx, &mgmntv1.UpdateMovieRequest{MovieId: movieID, Monitored: &monitored})
	return err
}

func musicMonitoredFromSource(source *listsyncv1.ListSource) bool {
	return normalizeMonitorMode(source.GetMonitorMode()) != monitorModeNone
}

func (m *Module) setArtistMonitored(ctx context.Context, artistID string, monitored bool) error {
	if err := m.ensureMusic(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	mc := m.musicClient
	m.mu.RUnlock()
	_, err := mc.UpdateArtist(ctx, &musicv1.UpdateArtistRequest{Id: artistID, Monitored: &monitored})
	return err
}

func (m *Module) setSeriesMonitored(ctx context.Context, seriesID string, monitored bool) error {
	if err := m.ensureTV(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	_, err := tc.UpdateTVShow(ctx, &tvmgmtv1.UpdateTVShowRequest{SeriesId: seriesID, Monitored: &monitored})
	return err
}

func (m *Module) applyTVFutureMonitor(ctx context.Context, seriesID string) error {
	if err := m.ensureTV(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	tc := m.tvClient
	m.mu.RUnlock()
	if _, err := tc.RefreshMetadata(ctx, &tvmgmtv1.RefreshMetadataRequest{SeriesId: seriesID}); err != nil {
		return err
	}
	show, err := tc.GetTVShow(ctx, &tvmgmtv1.GetTVShowRequest{SeriesId: seriesID})
	if err != nil {
		return err
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, season := range show.GetSeries().GetSeasons() {
		if season.GetSeasonNumber() <= 0 {
			continue
		}
		pastSeason := true
		for _, ep := range season.GetEpisodes() {
			air := strings.TrimSpace(ep.GetAirDate())
			if air == "" || air >= today {
				pastSeason = false
				break
			}
		}
		if pastSeason && len(season.GetEpisodes()) > 0 {
			mon := false
			if _, seasonErr := tc.UpdateSeasonMonitored(ctx, &tvmgmtv1.UpdateSeasonMonitoredRequest{
				SeasonId:  season.GetId(),
				Monitored: mon,
			}); seasonErr != nil {
				return seasonErr
			}
		}
	}
	mon := true
	_, err = tc.UpdateTVShow(ctx, &tvmgmtv1.UpdateTVShowRequest{SeriesId: seriesID, Monitored: &mon})
	return err
}

func (m *Module) ensureMaintainer(ctx context.Context) error {
	m.mu.RLock()
	if m.maintainerClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	addr, err := m.findCapabilityAddr(ctx, "media.library.maintainer")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial maintainer: %w", err)
	}
	m.mu.Lock()
	m.maintainerConn = conn
	m.maintainerClient = maintainv1.NewMaintainerServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) markSyncItemSkipped(ctx context.Context, sourceID, externalID, reason string) {
	now := time.Now().UTC().Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.ExecContext(ctx,
		`UPDATE sync_items SET status = 'skipped', matched_item_id = '', updated_at = ? WHERE source_id = ? AND external_id = ?`,
		now, sourceID, externalID,
	)
	if reason != "" {
		slog.Debug("import skipped", "source_id", sourceID, "external_id", externalID, "reason", reason)
	}
}

func sourceSearchOnAdd(source *listsyncv1.ListSource) bool {
	if source == nil {
		return true
	}
	return source.GetSearchOnAdd()
}

func addSourceSearchOnAdd(req *listsyncv1.AddSourceRequest) bool {
	if req.SearchOnAdd != nil {
		return req.GetSearchOnAdd()
	}
	return true
}
