package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	automationv1 "github.com/Muxcore-Media/media-automation/proto/automationv1"
	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	rootsv1 "github.com/Muxcore-Media/media-root-folders/proto/rootsv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
	metadatav1 "github.com/Muxcore-Media/metadata-tmdb/proto/metadatav1"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/core/sdk/go/client"
	_ "modernc.org/sqlite"
)

type Module struct {
	listsyncv1.UnimplementedListSyncServiceServer

	mu sync.RWMutex
	db *sql.DB
	mc *client.Client

	id       string
	dbPath   string
	grpcAddr string
	grpcSrv  *grpc.Server
	grpcLis  net.Listener
	httpCli  *http.Client

	automationConn   *grpc.ClientConn
	automationClient automationv1.AutomationServiceClient

	moviesConn   *grpc.ClientConn
	moviesClient mgmntv1.MovieManagementServiceClient
	tvConn       *grpc.ClientConn
	tvClient     tvmgmtv1.TvManagementServiceClient

	metadataConn   *grpc.ClientConn
	metadataClient metadatav1.MetadataServiceClient

	rootsConn   *grpc.ClientConn
	rootsClient rootsv1.RootFolderServiceClient
	rootsListFn func(ctx context.Context, mediaKind string) ([]string, error)

	// Optional test hooks for missing-episode queueing (nil = use mesh clients).
	refreshSeriesFn func(ctx context.Context, seriesID string) error
	listMissingFn   func(ctx context.Context, seriesID string, page, pageSize int32) (*tvmgmtv1.ListMissingResponse, error)
	addToQueueFn    func(ctx context.Context, req *automationv1.AddToQueueRequest) error
}

type Config struct {
	ID       string
	DBPath   string
	GRPCAddr string
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "media-list-sync"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "/var/lib/media-list-sync/listsync.db"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9530"
	}
	if v := os.Getenv("LISTSYNC_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("LISTSYNC_GRPC_ADDR"); v != "" {
		cfg.GRPCAddr = v
	}
	return &Module{
		id:       cfg.ID,
		dbPath:   cfg.DBPath,
		grpcAddr: cfg.GRPCAddr,
		httpCli:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:          m.id,
		Name:        "Media List Sync",
		Version:     "0.1.2",
		Roles:       []string{"listsync", "importlist"},
		Description: "Sync media watchlists and collections from Trakt, IMDb, Plex, Jellyfin, Radarr, and Sonarr",
		Author:      "MuxCore",
		Capabilities: []string{
			"media.listsync",
			"importlist.trakt",
			"importlist.imdb",
			"importlist.plex",
			"importlist.jellyfin",
			"importlist.radarr",
			"importlist.sonarr",
		},
		MinCoreVersion: "0.4.0",
		HTTPAddr:       m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	dir := filepath.Dir(m.dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", m.dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS list_sources (
			id                   TEXT PRIMARY KEY,
			name                 TEXT NOT NULL,
			type                 TEXT NOT NULL,
			enabled              INTEGER DEFAULT 1,
			username             TEXT DEFAULT '',
			client_id            TEXT DEFAULT '',
			list_url             TEXT DEFAULT '',
			sync_interval_minutes INTEGER DEFAULT 1440,
			last_synced          TEXT,
			created_at           TEXT NOT NULL,
			updated_at           TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return fmt.Errorf("create list_sources: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS sync_items (
			id               TEXT PRIMARY KEY,
			source_id        TEXT NOT NULL,
			external_id      TEXT NOT NULL,
			tmdb_id          INTEGER DEFAULT 0,
			imdb_id          TEXT DEFAULT '',
			media_type       TEXT DEFAULT '',
			title            TEXT DEFAULT '',
			year             INTEGER DEFAULT 0,
			season_number    INTEGER DEFAULT 0,
			episode_number   INTEGER DEFAULT 0,
			action           TEXT DEFAULT 'watchlist',
			status           TEXT DEFAULT 'pending',
			matched_item_id  TEXT DEFAULT '',
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL,
			UNIQUE(source_id, external_id)
		)
	`); err != nil {
		db.Close()
		return fmt.Errorf("create sync_items: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS sync_log (
			id           TEXT PRIMARY KEY,
			source_id    TEXT NOT NULL,
			source_name  TEXT DEFAULT '',
			status       TEXT DEFAULT 'running',
			items_found  INTEGER DEFAULT 0,
			items_new    INTEGER DEFAULT 0,
			error        TEXT DEFAULT '',
			started_at   TEXT NOT NULL,
			completed_at TEXT
		)
	`); err != nil {
		db.Close()
		return fmt.Errorf("create sync_log: %w", err)
	}

	for _, col := range []struct{ name, def string }{
		{"base_url", "TEXT DEFAULT ''"},
		{"api_key", "TEXT DEFAULT ''"},
		{"quality_profile_id", "TEXT DEFAULT ''"},
		{"root_folder_path", "TEXT DEFAULT ''"},
	} {
		_, _ = db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE list_sources ADD COLUMN %s %s`, col.name, col.def))
	}

	m.mu.Lock()
	m.db = db
	m.mu.Unlock()

	lis, err := net.Listen("tcp", m.grpcAddr)
	if err != nil {
		db.Close()
		return fmt.Errorf("listen gRPC: %w", err)
	}
	m.grpcLis = lis

	slog.Info("media-list-sync initialized", "db", m.dbPath, "grpc", m.grpcAddr)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.grpcSrv = grpc.NewServer()
	listsyncv1.RegisterListSyncServiceServer(m.grpcSrv, m)

	go func() {
		slog.Info("media-list-sync gRPC started", "addr", m.grpcAddr)
		if err := m.grpcSrv.Serve(m.grpcLis); err != nil {
			slog.Error("media-list-sync gRPC error", "error", err)
		}
	}()
	go m.syncLoop()
	go m.dialCore(context.Background())
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	if m.automationConn != nil {
		m.automationConn.Close()
	}
	if m.moviesConn != nil {
		m.moviesConn.Close()
	}
	if m.tvConn != nil {
		m.tvConn.Close()
	}
	if m.metadataConn != nil {
		m.metadataConn.Close()
	}
	if m.rootsConn != nil {
		_ = m.rootsConn.Close()
	}
	if m.mc != nil {
		m.mc.Close()
	}
	m.mu.Lock()
	if m.db != nil {
		m.db.Close()
		m.db = nil
	}
	m.mu.Unlock()
	slog.Info("media-list-sync stopped")
	return nil
}

func (m *Module) Health(ctx context.Context) error {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("not initialized")
	}
	return db.PingContext(ctx)
}

func (m *Module) dialCore(ctx context.Context) {
	meshAddr := os.Getenv("MUXCORE_GRPC_ADDR")
	if meshAddr == "" {
		meshAddr = "localhost:9090"
	}
	insecureMode := os.Getenv("MUXCORE_INSECURE_DISABLE_TLS") == "true" || os.Getenv("MUXCORE_GRPC_INSECURE") == "true"
	var opts []client.Option
	if insecureMode {
		opts = append(opts, client.WithInsecure())
	}
	c, err := client.Dial(meshAddr, opts...)
	if err != nil {
		slog.Error("media-list-sync: dial core", "error", err)
		return
	}
	m.mc = c
	slog.Info("media-list-sync: connected to core mesh", "addr", meshAddr)
}

func (m *Module) ensureAutomation(ctx context.Context) error {
	m.mu.RLock()
	if m.automationClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.automation")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial automation: %w", err)
	}
	m.mu.Lock()
	m.automationConn = conn
	m.automationClient = automationv1.NewAutomationServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureMovies(ctx context.Context) error {
	m.mu.RLock()
	if m.moviesClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.library.movies")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial movies: %w", err)
	}
	m.mu.Lock()
	m.moviesConn = conn
	m.moviesClient = mgmntv1.NewMovieManagementServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureTV(ctx context.Context) error {
	m.mu.RLock()
	if m.tvClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "media.library.tv")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial tvshows: %w", err)
	}
	m.mu.Lock()
	m.tvConn = conn
	m.tvClient = tvmgmtv1.NewTvManagementServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureMetadata(ctx context.Context) error {
	m.mu.RLock()
	if m.metadataClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()
	addr, err := m.findCapabilityAddr(ctx, "metadata")
	if err != nil {
		addr, err = m.findCapabilityAddr(ctx, "metadata.tmdb")
		if err != nil {
			return err
		}
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial metadata: %w", err)
	}
	m.mu.Lock()
	m.metadataConn = conn
	m.metadataClient = metadatav1.NewMetadataServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) findCapabilityAddr(ctx context.Context, capability string) (string, error) {
	if m.mc == nil {
		return "", fmt.Errorf("not connected to core")
	}
	modules, err := m.mc.Discovery.FindByCapability(ctx, capability)
	if err != nil {
		return "", fmt.Errorf("discover %s: %w", capability, err)
	}
	for _, mod := range modules {
		addr := dialAddrForModule(mod.Id, mod.HttpAddr)
		if addr != "" {
			return addr, nil
		}
	}
	return "", fmt.Errorf("no %s module found", capability)
}

// dialAddrForModule maps discovery HttpAddr to a dial target.
// Bare/wildcard hosts use module ID (compose DNS) unless MUXCORE_MESH_DIAL_LOCAL=true.
func dialAddrForModule(moduleID, httpAddr string) string {
	if httpAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(httpAddr)
	if err != nil || port == "" {
		return httpAddr
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return net.JoinHostPort(host, port)
	}
	if os.Getenv("MUXCORE_MESH_DIAL_LOCAL") == "true" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	if moduleID != "" {
		return net.JoinHostPort(moduleID, port)
	}
	return httpAddr
}

// ── Sync Loop ──────────────────────────────────────────────────

func (m *Module) syncLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	time.Sleep(10 * time.Second)
	m.syncAll()

	for range ticker.C {
		m.syncAll()
	}
}

func (m *Module) syncAll() (found, newItems int) {
	return m.syncAllForced(false)
}

func (m *Module) syncAllForced(force bool) (found, newItems int) {
	m.mu.RLock()
	sources := m.loadSources()
	m.mu.RUnlock()

	for _, s := range sources {
		if !s.GetEnabled() {
			continue
		}
		if !force && s.GetLastSynced() != "" {
			lastSync, err := time.Parse(time.RFC3339, s.GetLastSynced())
			if err == nil {
				interval := time.Duration(s.GetSyncIntervalMinutes()) * time.Minute
				if interval == 0 {
					interval = 24 * time.Hour
				}
				if time.Since(lastSync) < interval {
					continue
				}
			}
		}
		f, n := m.syncSource(s)
		found += f
		newItems += n
	}
	return found, newItems
}

func (m *Module) syncSource(source *listsyncv1.ListSource) (found, newCount int) {
	logID := fmt.Sprintf("log_%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339)

	m.mu.Lock()
	m.db.Exec(`INSERT INTO sync_log (id, source_id, source_name, status, started_at) VALUES (?, ?, ?, 'running', ?)`,
		logID, source.GetId(), source.GetName(), now)
	m.mu.Unlock()

	var items []syncItem
	var err error

	switch source.GetType() {
	case "trakt":
		items, err = m.fetchTrakt(source)
	case "imdb":
		items, err = m.fetchIMDb(source)
	case "plex":
		items, err = m.fetchPlex(source)
	case "jellyfin":
		items, err = m.fetchJellyfin(source)
	case "radarr":
		items, err = m.fetchRadarr(source)
	case "sonarr":
		items, err = m.fetchSonarr(source)
	default:
		err = fmt.Errorf("unknown source type: %s", source.GetType())
	}

	if err == nil {
		newCount = m.storeItems(source.GetId(), items)
		found = len(items)
	}

	completed := time.Now().UTC().Format(time.RFC3339)
	status := "completed"
	errStr := ""
	if err != nil {
		status = "failed"
		errStr = err.Error()
		slog.Error("sync failed", "source", source.GetName(), "error", err)
	}

	m.mu.Lock()
	m.db.Exec(`UPDATE sync_log SET status = ?, items_found = ?, items_new = ?, error = ?, completed_at = ? WHERE id = ?`,
		status, found, newCount, errStr, completed, logID)
	m.db.Exec(`UPDATE list_sources SET last_synced = ?, updated_at = ? WHERE id = ?`, now, now, source.GetId())
	m.mu.Unlock()

	if err == nil && len(items) > 0 {
		m.addItemsToLibrary(source, items)
	}

	slog.Info("sync completed", "source", source.GetName(), "found", found, "new", newCount)
	return found, newCount
}

func (m *Module) addItemsToLibrary(source *listsyncv1.ListSource, items []syncItem) {
	sourceID := source.GetId()
	var fallback []syncItem
	for _, item := range items {
		if m.syncItemStatus(sourceID, item.ExternalID) == "added" {
			// Re-syncs: still queue missing monitored episodes for known series.
			if item.MediaType == "tv" {
				if matchedID := m.syncItemMatchedID(sourceID, item.ExternalID); matchedID != "" {
					go m.queueMissingMonitoredEpisodes(matchedID, item)
				}
			}
			continue
		}
		if item.TMDBID <= 0 {
			if resolved, ok := m.resolveTMDB(item); ok {
				item = resolved
			}
		}
		if item.TMDBID <= 0 {
			fallback = append(fallback, item)
			continue
		}
		matchedID, err := m.addLibraryItem(source, item)
		if err != nil {
			slog.Debug("library add failed", "title", item.Title, "error", err)
			fallback = append(fallback, item)
			continue
		}
		m.markSyncItemAdded(sourceID, item.ExternalID, matchedID)
		if item.MediaType == "tv" && matchedID != "" {
			go m.queueMissingMonitoredEpisodes(matchedID, item)
		}
	}
	if len(fallback) > 0 {
		m.queueWithAutomation(fallback)
	}
}

func (m *Module) syncItemStatus(sourceID, externalID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return ""
	}
	var status string
	_ = m.db.QueryRow(
		`SELECT status FROM sync_items WHERE source_id = ? AND external_id = ?`,
		sourceID, externalID,
	).Scan(&status)
	return status
}

func (m *Module) syncItemMatchedID(sourceID, externalID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.db == nil {
		return ""
	}
	var matched string
	_ = m.db.QueryRow(
		`SELECT matched_item_id FROM sync_items WHERE source_id = ? AND external_id = ?`,
		sourceID, externalID,
	).Scan(&matched)
	return matched
}

func (m *Module) resolveTMDB(item syncItem) (syncItem, bool) {
	if item.IMDBID == "" {
		return item, false
	}
	ctx := context.Background()
	if err := m.ensureMetadata(ctx); err != nil {
		slog.Debug("metadata unavailable for resolve", "error", err)
		return item, false
	}
	m.mu.RLock()
	mc := m.metadataClient
	m.mu.RUnlock()

	resp, err := mc.FindByExternalID(ctx, &metadatav1.FindByExternalIDRequest{
		ExternalId:     item.IMDBID,
		ExternalSource: "imdb_id",
	})
	if err != nil || len(resp.GetResults()) == 0 {
		if item.Title != "" {
			return m.resolveViaSearch(item)
		}
		return item, false
	}

	wantMovie := item.MediaType == "movie" || item.MediaType == ""
	wantTV := item.MediaType == "tv"
	var pick *metadatav1.SearchResult
	for _, r := range resp.GetResults() {
		switch r.GetMediaType() {
		case metadatav1.MediaType_MEDIA_TYPE_MOVIE:
			if wantMovie {
				pick = r
			}
		case metadatav1.MediaType_MEDIA_TYPE_TV:
			if wantTV {
				pick = r
			}
		}
		if pick != nil {
			break
		}
	}
	if pick == nil {
		pick = resp.GetResults()[0]
	}
	item.TMDBID = int(pick.GetId())
	if item.Title == "" {
		if pick.GetTitle() != "" {
			item.Title = pick.GetTitle()
		} else {
			item.Title = pick.GetName()
		}
	}
	if item.Year == 0 {
		item.Year = yearFromDate(pick.GetReleaseDate())
		if item.Year == 0 {
			item.Year = yearFromDate(pick.GetFirstAirDate())
		}
	}
	if item.MediaType == "" {
		if pick.GetMediaType() == metadatav1.MediaType_MEDIA_TYPE_TV {
			item.MediaType = "tv"
		} else {
			item.MediaType = "movie"
		}
	}
	m.updateSyncItemTMDB(item)
	return item, true
}

func (m *Module) resolveViaSearch(item syncItem) (syncItem, bool) {
	ctx := context.Background()
	m.mu.RLock()
	mc := m.metadataClient
	m.mu.RUnlock()
	if mc == nil {
		return item, false
	}
	mediaType := metadatav1.MediaType_MEDIA_TYPE_UNSPECIFIED
	switch item.MediaType {
	case "movie":
		mediaType = metadatav1.MediaType_MEDIA_TYPE_MOVIE
	case "tv":
		mediaType = metadatav1.MediaType_MEDIA_TYPE_TV
	}
	resp, err := mc.Search(ctx, &metadatav1.SearchRequest{
		Query: item.Title,
		Type:  mediaType,
		Page:  1,
		Year:  int32(item.Year),
	})
	if err != nil || len(resp.GetResults()) == 0 {
		return item, false
	}
	pick := resp.GetResults()[0]
	if item.Year > 0 {
		for _, r := range resp.GetResults() {
			y := yearFromDate(r.GetReleaseDate())
			if y == 0 {
				y = yearFromDate(r.GetFirstAirDate())
			}
			if y == item.Year {
				pick = r
				break
			}
		}
	}
	item.TMDBID = int(pick.GetId())
	m.updateSyncItemTMDB(item)
	return item, true
}

func yearFromDate(s string) int {
	if len(s) < 4 {
		return 0
	}
	var y int
	fmt.Sscanf(s[:4], "%d", &y)
	return y
}

func (m *Module) updateSyncItemTMDB(item syncItem) {
	now := time.Now().UTC().Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.Exec(
		`UPDATE sync_items SET tmdb_id = ?, title = ?, year = ?, media_type = ?, updated_at = ? WHERE external_id = ?`,
		item.TMDBID, item.Title, item.Year, item.MediaType, now, item.ExternalID,
	)
}

func (m *Module) addLibraryItem(source *listsyncv1.ListSource, item syncItem) (string, error) {
	ctx := context.Background()
	switch item.MediaType {
	case "movie":
		if err := m.ensureMovies(ctx); err != nil {
			return "", err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		resp, err := mc.AddMovie(ctx, &mgmntv1.AddMovieRequest{
			TmdbId:           int32(item.TMDBID),
			Title:            item.Title,
			Year:             int32(item.Year),
			QualityProfileId: source.GetQualityProfileId(),
			RootFolderPath:   source.GetRootFolderPath(),
		})
		if err != nil {
			return "", err
		}
		return resp.GetMovieId(), nil
	case "tv":
		if err := m.ensureTV(ctx); err != nil {
			return "", err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		resp, err := tc.AddTVShow(ctx, &tvmgmtv1.AddTVShowRequest{
			TmdbId:           int32(item.TMDBID),
			Name:             item.Title,
			Year:             int32(item.Year),
			QualityProfileId: source.GetQualityProfileId(),
			RootFolderPath:   source.GetRootFolderPath(),
		})
		if err != nil {
			return "", err
		}
		return resp.GetSeriesId(), nil
	default:
		return "", fmt.Errorf("unsupported media type %q", item.MediaType)
	}
}

func (m *Module) markSyncItemAdded(sourceID, externalID, matchedID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.db == nil {
		return
	}
	_, _ = m.db.Exec(
		`UPDATE sync_items SET status = 'added', matched_item_id = ?, updated_at = ? WHERE source_id = ? AND external_id = ?`,
		matchedID, now, sourceID, externalID,
	)
}

func (m *Module) queueWithAutomation(items []syncItem) {
	if err := m.ensureAutomation(context.Background()); err != nil {
		slog.Debug("automation not available for queueing", "error", err)
		return
	}
	m.mu.RLock()
	ac := m.automationClient
	m.mu.RUnlock()

	for _, item := range items {
		_, err := ac.AddToQueue(context.Background(), &automationv1.AddToQueueRequest{
			ItemType:      item.MediaType,
			ItemId:        item.ExternalID,
			TmdbId:        int32(item.TMDBID),
			Title:         item.Title,
			Year:          int32(item.Year),
			SeasonNumber:  int32(item.SeasonNumber),
			EpisodeNumber: int32(item.EpisodeNumber),
		})
		if err != nil {
			slog.Debug("queue item failed", "title", item.Title, "error", err)
		}
	}
}

// queueMissingMonitoredEpisodes refreshes series metadata when needed, then
// enqueues each missing monitored episode (not just the series row).
func (m *Module) queueMissingMonitoredEpisodes(seriesID string, series syncItem) {
	ctx := context.Background()

	refresh := m.refreshSeriesFn
	listMissing := m.listMissingFn
	addQueue := m.addToQueueFn

	if refresh == nil || listMissing == nil || addQueue == nil {
		if err := m.ensureTV(ctx); err != nil {
			slog.Debug("tv module unavailable for missing-episode queue", "series_id", seriesID, "error", err)
			return
		}
		if err := m.ensureAutomation(ctx); err != nil {
			slog.Debug("automation unavailable for missing-episode queue", "series_id", seriesID, "error", err)
			return
		}
		m.mu.RLock()
		tc := m.tvClient
		ac := m.automationClient
		m.mu.RUnlock()
		if tc == nil || ac == nil {
			return
		}
		if refresh == nil {
			refresh = func(ctx context.Context, seriesID string) error {
				_, err := tc.RefreshMetadata(ctx, &tvmgmtv1.RefreshMetadataRequest{SeriesId: seriesID})
				return err
			}
		}
		if listMissing == nil {
			listMissing = func(ctx context.Context, seriesID string, page, pageSize int32) (*tvmgmtv1.ListMissingResponse, error) {
				return tc.ListMissing(ctx, &tvmgmtv1.ListMissingRequest{
					Page: page, PageSize: pageSize, SeriesId: seriesID,
				})
			}
		}
		if addQueue == nil {
			addQueue = func(ctx context.Context, req *automationv1.AddToQueueRequest) error {
				_, err := ac.AddToQueue(ctx, req)
				return err
			}
		}
	}

	// AddTVShow populates episodes asynchronously; RefreshMetadata fills them synchronously.
	if err := refresh(ctx, seriesID); err != nil {
		slog.Debug("refresh before missing queue failed", "series_id", seriesID, "error", err)
	}

	var queued int
	page := int32(1)
	const pageSize = int32(100)
	for {
		resp, err := listMissing(ctx, seriesID, page, pageSize)
		if err != nil {
			slog.Debug("ListMissing failed", "series_id", seriesID, "error", err)
			return
		}
		for _, ep := range resp.GetItems() {
			title := ep.GetTitle()
			if title == "" {
				title = series.Title
			}
			tmdbID := ep.GetTmdbId()
			if tmdbID == 0 {
				tmdbID = int32(series.TMDBID)
			}
			year := ep.GetYear()
			if year == 0 {
				year = int32(series.Year)
			}
			err := addQueue(ctx, &automationv1.AddToQueueRequest{
				ItemType:      "tv",
				ItemId:        ep.GetEpisodeId(),
				TmdbId:        tmdbID,
				Title:         title,
				Year:          year,
				SeasonNumber:  ep.GetSeasonNumber(),
				EpisodeNumber: ep.GetEpisodeNumber(),
			})
			if err != nil {
				slog.Debug("queue missing episode failed",
					"series_id", seriesID,
					"episode_id", ep.GetEpisodeId(),
					"error", err,
				)
				continue
			}
			queued++
		}
		if int32(len(resp.GetItems())) < pageSize || page*pageSize >= resp.GetTotal() {
			break
		}
		page++
	}
	if queued > 0 {
		slog.Info("queued missing monitored episodes after list sync",
			"series_id", seriesID, "queued", queued, "title", series.Title)
	}
}

// ── gRPC API ───────────────────────────────────────────────────

func (m *Module) ListSources(ctx context.Context, req *listsyncv1.ListSourcesRequest) (*listsyncv1.ListSourcesResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &listsyncv1.ListSourcesResponse{Sources: m.loadSources()}, nil
}

func (m *Module) AddSource(ctx context.Context, req *listsyncv1.AddSourceRequest) (*listsyncv1.AddSourceResponse, error) {
	rootPath, err := m.resolveRootFolderPath(ctx, req.GetRootFolderPath(), "")
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	id := fmt.Sprintf("ls_%d", time.Now().UnixNano())

	interval := req.GetSyncIntervalMinutes()
	if interval <= 0 {
		interval = 1440
	}

	_, err = m.db.Exec(`INSERT INTO list_sources (id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, base_url, api_key, quality_profile_id, root_folder_path, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.GetName(), req.GetType(), req.GetUsername(), req.GetClientId(), req.GetListUrl(), interval,
		req.GetBaseUrl(), req.GetApiKey(), req.GetQualityProfileId(), rootPath, now, now)
	if err != nil {
		return nil, fmt.Errorf("add source: %w", err)
	}

	return &listsyncv1.AddSourceResponse{Source: m.loadSource(id)}, nil
}

func (m *Module) RemoveSource(ctx context.Context, req *listsyncv1.RemoveSourceRequest) (*listsyncv1.RemoveSourceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db.Exec(`DELETE FROM sync_items WHERE source_id = ?`, req.GetId())
	m.db.Exec(`DELETE FROM list_sources WHERE id = ?`, req.GetId())
	return &listsyncv1.RemoveSourceResponse{}, nil
}

func (m *Module) SyncNow(ctx context.Context, req *listsyncv1.SyncNowRequest) (*listsyncv1.SyncNowResponse, error) {
	found, newItems := m.syncAllForced(true)
	return &listsyncv1.SyncNowResponse{ItemsFound: int32(found), ItemsNew: int32(newItems)}, nil
}

func (m *Module) GetHistory(ctx context.Context, req *listsyncv1.GetHistoryRequest) (*listsyncv1.GetHistoryResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	m.db.QueryRow(`SELECT COUNT(*) FROM sync_log`).Scan(&total)

	rows, err := m.db.Query(`SELECT id, source_id, source_name, status, items_found, items_new, error, started_at, completed_at FROM sync_log ORDER BY started_at DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var entries []*listsyncv1.SyncLogEntry
	for rows.Next() {
		var id, srcID, srcName, status, startedAt string
		var found, newCount int64
		var errStr, completedAt sql.NullString
		if err := rows.Scan(&id, &srcID, &srcName, &status, &found, &newCount, &errStr, &startedAt, &completedAt); err != nil {
			continue
		}
		entries = append(entries, &listsyncv1.SyncLogEntry{
			Id: id, SourceId: srcID, SourceName: srcName, Status: status,
			ItemsFound: int32(found), ItemsNew: int32(newCount), Error: errStr.String,
			StartedAt: startedAt, CompletedAt: completedAt.String,
		})
	}

	return &listsyncv1.GetHistoryResponse{Entries: entries, Total: int32(total)}, nil
}

func (m *Module) GetItems(ctx context.Context, req *listsyncv1.GetItemsRequest) (*listsyncv1.GetItemsResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	page := int(req.GetPage())
	if page < 1 {
		page = 1
	}
	pageSize := int(req.GetPageSize())
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	query := `SELECT id, source_id, external_id, tmdb_id, imdb_id, media_type, title, year, season_number, episode_number, action, status, matched_item_id, created_at, updated_at FROM sync_items`
	countQuery := `SELECT COUNT(*) FROM sync_items`
	var args []any
	var where []string

	if req.GetSourceId() != "" {
		where = append(where, `source_id = ?`)
		args = append(args, req.GetSourceId())
	}
	if req.GetMediaType() != "" {
		where = append(where, `media_type = ?`)
		args = append(args, req.GetMediaType())
	}
	if req.GetMatchedOnly() {
		where = append(where, `status = 'matched'`)
	}
	if len(where) > 0 {
		clause := ` WHERE ` + strings.Join(where, ` AND `)
		query += clause
		countQuery += clause
	}

	var total int
	m.db.QueryRow(countQuery, args...).Scan(&total)
	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	qargs := append(args, pageSize, offset)

	rows, err := m.db.Query(query, qargs...)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []*listsyncv1.SyncItem
	for rows.Next() {
		item := scanSyncItem(rows)
		if item != nil {
			items = append(items, item)
		}
	}
	return &listsyncv1.GetItemsResponse{Items: items, Total: int32(total), Page: int32(page), PageSize: int32(pageSize)}, nil
}

// ── Trakt Integration ──────────────────────────────────────────

type traktIDs struct {
	Trakt int    `json:"trakt"`
	IMDb  string `json:"imdb"`
	TMDB  int    `json:"tmdb"`
}

type traktMovie struct {
	IDs   traktIDs `json:"ids"`
	Title string   `json:"title"`
	Year  int      `json:"year"`
}

type traktShow struct {
	IDs   traktIDs `json:"ids"`
	Title string   `json:"title"`
	Year  int      `json:"year"`
}

type traktWatchlistMovie struct {
	Movie traktMovie `json:"movie"`
}

type traktWatchlistShow struct {
	Show traktShow `json:"show"`
}

type syncItem struct {
	ExternalID    string
	IMDBID        string
	TMDBID        int
	MediaType     string
	Title         string
	Year          int
	SeasonNumber  int
	EpisodeNumber int
	Action        string
}

func (m *Module) fetchTrakt(source *listsyncv1.ListSource) ([]syncItem, error) {
	clientID := source.GetClientId()
	if clientID == "" {
		return nil, fmt.Errorf("trakt client_id not configured for source %s", source.GetName())
	}
	accessToken := source.GetApiKey()
	if accessToken == "" {
		return nil, fmt.Errorf("trakt api_key (OAuth access token) not configured for source %s", source.GetName())
	}

	var items []syncItem

	movies, err := traktGet[traktWatchlistMovie](m.httpCli, clientID, accessToken, "/sync/watchlist/movies")
	if err != nil {
		return nil, fmt.Errorf("trakt watchlist movies: %w", err)
	}
	for _, entry := range movies {
		mv := entry.Movie
		if mv.IDs.Trakt == 0 && mv.Title == "" {
			continue
		}
		items = append(items, syncItem{
			ExternalID: fmt.Sprintf("trakt_%d", mv.IDs.Trakt),
			IMDBID:     mv.IDs.IMDb,
			TMDBID:     mv.IDs.TMDB,
			MediaType:  "movie",
			Title:      mv.Title,
			Year:       mv.Year,
			Action:     "watchlist",
		})
	}

	shows, err := traktGet[traktWatchlistShow](m.httpCli, clientID, accessToken, "/sync/watchlist/shows")
	if err != nil {
		return nil, fmt.Errorf("trakt watchlist shows: %w", err)
	}
	for _, entry := range shows {
		show := entry.Show
		if show.IDs.Trakt == 0 && show.Title == "" {
			continue
		}
		items = append(items, syncItem{
			ExternalID: fmt.Sprintf("trakt_%d", show.IDs.Trakt),
			IMDBID:     show.IDs.IMDb,
			TMDBID:     show.IDs.TMDB,
			MediaType:  "tv",
			Title:      show.Title,
			Year:       show.Year,
			Action:     "watchlist",
		})
	}

	return items, nil
}

func traktGet[T any](cli *http.Client, clientID, accessToken, path string) ([]T, error) {
	base := "https://api.trakt.tv"
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", clientID)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("trakt API %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var result []T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return result, nil
}

// ── IMDb Integration ───────────────────────────────────────────

func (m *Module) fetchIMDb(source *listsyncv1.ListSource) ([]syncItem, error) {
	listURL := source.GetListUrl()
	if listURL == "" {
		return nil, fmt.Errorf("imdb list_url not configured for source %s", source.GetName())
	}

	// IMDb exports CSV at /list/<id>/export
	if !strings.Contains(listURL, "/export") {
		u, err := url.Parse(listURL)
		if err != nil {
			return nil, fmt.Errorf("invalid imdb URL: %w", err)
		}
		listURL = fmt.Sprintf("https://www.imdb.com/list/%s/export", strings.TrimPrefix(u.Path, "/list/"))
	}

	resp, err := m.httpCli.Get(listURL)
	if err != nil {
		return nil, fmt.Errorf("fetch imdb list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseIMDbCSV(string(body))
}

func parseIMDbCSV(data string) ([]syncItem, error) {
	lines := strings.Split(data, "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	var items []syncItem
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := splitCSVLine(line)
		if len(fields) < 5 {
			continue
		}
		// IMDb CSV: position,const,created,modified,title,title_type,url,imdbid,...
		// Fields: 0=position, 1=const (tt...), 2=created, 3=modified, 4=title, 5=titleType, ...
		constID := strings.TrimSpace(fields[1])
		title := strings.TrimSpace(fields[4])
		titleType := strings.TrimSpace(fields[5])
		year := 0

		if len(fields) > 8 {
			yearStr := strings.TrimSpace(fields[8])
			fmt.Sscanf(yearStr, "%d", &year)
		}

		mediaType := "movie"
		if titleType == "tvSeries" || titleType == "tvMiniSeries" || titleType == "tvMovie" {
			mediaType = "tv"
		}

		items = append(items, syncItem{
			ExternalID: constID,
			IMDBID:     constID,
			MediaType:  mediaType,
			Title:      title,
			Year:       year,
			Action:     "watchlist",
		})
	}
	return items, nil
}

func splitCSVLine(line string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ',' && !inQuote:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		fields = append(fields, cur.String())
	}
	return fields
}

// ── DB helpers ─────────────────────────────────────────────────

func (m *Module) storeItems(sourceID string, items []syncItem) int {
	if len(items) == 0 {
		return 0
	}

	newCount := 0
	now := time.Now().UTC().Format(time.RFC3339)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, item := range items {
		id := fmt.Sprintf("si_%d", time.Now().UnixNano()+int64(newCount))
		_, err := m.db.Exec(`INSERT OR IGNORE INTO sync_items (id, source_id, external_id, tmdb_id, imdb_id, media_type, title, year, season_number, episode_number, action, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			id, sourceID, item.ExternalID, item.TMDBID, item.IMDBID, item.MediaType, item.Title, item.Year,
			item.SeasonNumber, item.EpisodeNumber, item.Action, now, now)
		if err == nil {
			rows, _ := m.db.Exec(`SELECT changes()`)
			if affected, _ := rows.RowsAffected(); affected > 0 {
				newCount++
			}
		}
	}
	return newCount
}

func (m *Module) loadSources() []*listsyncv1.ListSource {
	rows, err := m.db.Query(`SELECT id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, last_synced, created_at, updated_at, COALESCE(base_url,''), COALESCE(api_key,''), COALESCE(quality_profile_id,''), COALESCE(root_folder_path,'') FROM list_sources ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sources []*listsyncv1.ListSource
	for rows.Next() {
		var id, name, stype, username, clientID, listURL, createdAt, updatedAt string
		var baseURL, apiKey, qualityProfileID, rootFolderPath string
		var enabled int64
		var interval int64
		var lastSynced sql.NullString
		if err := rows.Scan(&id, &name, &stype, &enabled, &username, &clientID, &listURL, &interval, &lastSynced, &createdAt, &updatedAt, &baseURL, &apiKey, &qualityProfileID, &rootFolderPath); err != nil {
			continue
		}
		sources = append(sources, &listsyncv1.ListSource{
			Id: id, Name: name, Type: stype, Enabled: enabled != 0,
			Username: username, ClientId: clientID, ListUrl: listURL,
			SyncIntervalMinutes: int32(interval), LastSynced: lastSynced.String,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
			BaseUrl: baseURL, ApiKey: apiKey,
			QualityProfileId: qualityProfileID, RootFolderPath: rootFolderPath,
		})
	}
	return sources
}

func (m *Module) loadSource(id string) *listsyncv1.ListSource {
	row := m.db.QueryRow(`SELECT id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, last_synced, created_at, updated_at, COALESCE(base_url,''), COALESCE(api_key,''), COALESCE(quality_profile_id,''), COALESCE(root_folder_path,'') FROM list_sources WHERE id = ?`, id)
	var srcID, name, stype, username, clientID, listURL, createdAt, updatedAt string
	var baseURL, apiKey, qualityProfileID, rootFolderPath string
	var enabled int64
	var interval int64
	var lastSynced sql.NullString
	if err := row.Scan(&srcID, &name, &stype, &enabled, &username, &clientID, &listURL, &interval, &lastSynced, &createdAt, &updatedAt, &baseURL, &apiKey, &qualityProfileID, &rootFolderPath); err != nil {
		return nil
	}
	return &listsyncv1.ListSource{
		Id: srcID, Name: name, Type: stype, Enabled: enabled != 0,
		Username: username, ClientId: clientID, ListUrl: listURL,
		SyncIntervalMinutes: int32(interval), LastSynced: lastSynced.String,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
		BaseUrl: baseURL, ApiKey: apiKey,
		QualityProfileId: qualityProfileID, RootFolderPath: rootFolderPath,
	}
}

func scanSyncItem(rows *sql.Rows) *listsyncv1.SyncItem {
	var id, srcID, extID, imdbID, mediaType, title, action, status, matchedID, createdAt, updatedAt string
	var tmdbID, year, seasonNum, epNum int64
	if err := rows.Scan(&id, &srcID, &extID, &tmdbID, &imdbID, &mediaType, &title, &year, &seasonNum, &epNum, &action, &status, &matchedID, &createdAt, &updatedAt); err != nil {
		return nil
	}
	return &listsyncv1.SyncItem{
		Id: id, SourceId: srcID, ExternalId: extID, TmdbId: int32(tmdbID), ImdbId: imdbID,
		MediaType: mediaType, Title: title, Year: int32(year),
		SeasonNumber: int32(seasonNum), EpisodeNumber: int32(epNum),
		Action: action, Status: status, MatchedItemId: matchedID,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ contracts.Module = (*Module)(nil)
