package internal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/pkg/contracts"
)

const (
	settingPollInterval = "poll_interval"
	syncLogMaxEntries   = 1000
	syncLogMaxAge       = 30 * 24 * time.Hour
)

func (m *Module) Settings() []contracts.SettingDef {
	return m.settingsDefs()
}

func (m *Module) UpdateSetting(key, value string) error {
	return m.updateSetting(key, value)
}

func (m *Module) settingsDefs() []contracts.SettingDef {
	return []contracts.SettingDef{
		{
			Key:         settingPollInterval,
			Label:       "Sync Poll Interval",
			Type:        contracts.SettingTypeString,
			Value:       m.getPollInterval().String(),
			Description: "How often to scan list sources for due syncs (Go duration; LISTSYNC_POLL_INTERVAL)",
			Group:       "Sync",
		},
	}
}

func (m *Module) updateSetting(key, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case settingPollInterval, "LISTSYNC_POLL_INTERVAL":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid poll_interval %q (use Go duration like 15m)", value)
		}
		m.cfgMu.Lock()
		m.pollInterval = d
		m.cfgMu.Unlock()
		return m.persistSetting(context.Background(), settingPollInterval, d.String())
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
}

func (m *Module) getPollInterval() time.Duration {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	if m.pollInterval <= 0 {
		return 15 * time.Minute
	}
	return m.pollInterval
}

func (m *Module) loadPersistedSettings(ctx context.Context) error {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM module_settings WHERE key = ?`, settingPollInterval).Scan(&raw)
	if err != nil {
		return nil
	}
	d, parseErr := time.ParseDuration(strings.TrimSpace(raw))
	if parseErr != nil || d <= 0 {
		return nil
	}
	m.cfgMu.Lock()
	m.pollInterval = d
	m.cfgMu.Unlock()
	return nil
}

func (m *Module) persistSetting(ctx context.Context, key, value string) error {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return fmt.Errorf("db not initialized")
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO module_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (m *Module) pruneSyncLog(ctx context.Context) {
	if m.db == nil {
		return
	}
	cutoff := time.Now().UTC().Add(-syncLogMaxAge).Format(time.RFC3339)
	_, _ = m.db.ExecContext(ctx, `DELETE FROM sync_log WHERE started_at < ?`, cutoff)
	_, _ = m.db.ExecContext(ctx, `
		DELETE FROM sync_log WHERE id IN (
			SELECT id FROM sync_log ORDER BY started_at DESC LIMIT -1 OFFSET ?
		)
	`, syncLogMaxEntries)
}
