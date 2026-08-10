package internal

import (
	"fmt"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/pkg/contracts"
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
			Key:         "poll_interval",
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
	case "poll_interval", "LISTSYNC_POLL_INTERVAL":
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid poll_interval %q (use Go duration like 15m)", value)
		}
		m.cfgMu.Lock()
		m.pollInterval = d
		m.cfgMu.Unlock()
		return nil
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
