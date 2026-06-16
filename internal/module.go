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
		ID:             m.id,
		Name:           "Media List Sync",
		Version:        "0.1.0",
		Roles:          []string{"listsync"},
		Description:    "Sync media watchlists and collections from Trakt, IMDb, and other providers",
		Author:         "MuxCore",
		Capabilities:   []string{"media.listsync"},
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
	insecureMode := os.Getenv("MUXCORE_GRPC_INSECURE") == "true"
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
	if m.mc == nil {
		return fmt.Errorf("not connected to core")
	}
	modules, err := m.mc.Discovery.FindByCapability(ctx, "media.automation")
	if err != nil {
		return fmt.Errorf("discover automation: %w", err)
	}
	var addr string
	for _, mod := range modules {
		if mod.HttpAddr != "" {
			addr = mod.HttpAddr
			break
		}
	}
	if addr == "" {
		return fmt.Errorf("no automation module found")
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

func (m *Module) syncAll() {
	m.mu.RLock()
	sources := m.loadSources()
	m.mu.RUnlock()

	for _, s := range sources {
		if !s.GetEnabled() {
			continue
		}
		if s.GetLastSynced() != "" {
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
		m.syncSource(s)
	}
}

func (m *Module) syncSource(source *listsyncv1.ListSource) {
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
	default:
		err = fmt.Errorf("unknown source type: %s", source.GetType())
	}

	newCount := 0
	if err == nil {
		newCount = m.storeItems(source.GetId(), items)
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
		status, len(items), newCount, errStr, completed, logID)
	m.db.Exec(`UPDATE list_sources SET last_synced = ?, updated_at = ? WHERE id = ?`, now, now, source.GetId())
	m.mu.Unlock()

	if err == nil && len(items) > 0 {
		m.queueWithAutomation(items)
	}

	slog.Info("sync completed", "source", source.GetName(), "found", len(items), "new", newCount)
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

// ── gRPC API ───────────────────────────────────────────────────

func (m *Module) ListSources(ctx context.Context, req *listsyncv1.ListSourcesRequest) (*listsyncv1.ListSourcesResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &listsyncv1.ListSourcesResponse{Sources: m.loadSources()}, nil
}

func (m *Module) AddSource(ctx context.Context, req *listsyncv1.AddSourceRequest) (*listsyncv1.AddSourceResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	id := fmt.Sprintf("ls_%d", time.Now().UnixNano())

	interval := req.GetSyncIntervalMinutes()
	if interval <= 0 {
		interval = 1440
	}

	_, err := m.db.Exec(`INSERT INTO list_sources (id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?)`,
		id, req.GetName(), req.GetType(), req.GetUsername(), req.GetClientId(), req.GetListUrl(), interval, now, now)
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
	m.syncAll()
	return &listsyncv1.SyncNowResponse{}, nil
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

type traktMovie struct {
	IDs struct {
		Trakt int    `json:"trakt"`
		IMDb  string `json:"imdb"`
		TMDB  int    `json:"tmdb"`
	} `json:"ids"`
	Title string `json:"title"`
	Year  int    `json:"year"`
}

type traktShow struct {
	IDs struct {
		Trakt int    `json:"trakt"`
		IMDb  string `json:"imdb"`
		TMDB  int    `json:"tmdb"`
	} `json:"ids"`
	Title string `json:"title"`
	Year  int    `json:"year"`
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

	var items []syncItem

	movies, err := traktGet[traktMovie](m.httpCli, clientID, "/sync/watchlist/movies")
	if err != nil {
		return nil, fmt.Errorf("trakt watchlist movies: %w", err)
	}
	for _, mv := range movies {
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

	shows, err := traktGet[traktShow](m.httpCli, clientID, "/sync/watchlist/shows")
	if err != nil {
		return nil, fmt.Errorf("trakt watchlist shows: %w", err)
	}
	for _, show := range shows {
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

func traktGet[T any](cli *http.Client, clientID, path string) ([]T, error) {
	base := "https://api.trakt.tv"
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", "2")
	req.Header.Set("trakt-api-key", clientID)

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
	rows, err := m.db.Query(`SELECT id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, last_synced, created_at, updated_at FROM list_sources ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sources []*listsyncv1.ListSource
	for rows.Next() {
		var id, name, stype, username, clientID, listURL, createdAt, updatedAt string
		var enabled int64
		var interval int64
		var lastSynced sql.NullString
		if err := rows.Scan(&id, &name, &stype, &enabled, &username, &clientID, &listURL, &interval, &lastSynced, &createdAt, &updatedAt); err != nil {
			continue
		}
		sources = append(sources, &listsyncv1.ListSource{
			Id: id, Name: name, Type: stype, Enabled: enabled != 0,
			Username: username, ClientId: clientID, ListUrl: listURL,
			SyncIntervalMinutes: int32(interval), LastSynced: lastSynced.String,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		})
	}
	return sources
}

func (m *Module) loadSource(id string) *listsyncv1.ListSource {
	row := m.db.QueryRow(`SELECT id, name, type, enabled, username, client_id, list_url, sync_interval_minutes, last_synced, created_at, updated_at FROM list_sources WHERE id = ?`, id)
	var srcID, name, stype, username, clientID, listURL, createdAt, updatedAt string
	var enabled int64
	var interval int64
	var lastSynced sql.NullString
	if err := row.Scan(&srcID, &name, &stype, &enabled, &username, &clientID, &listURL, &interval, &lastSynced, &createdAt, &updatedAt); err != nil {
		return nil
	}
	return &listsyncv1.ListSource{
		Id: srcID, Name: name, Type: stype, Enabled: enabled != 0,
		Username: username, ClientId: clientID, ListUrl: listURL,
		SyncIntervalMinutes: int32(interval), LastSynced: lastSynced.String,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
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
