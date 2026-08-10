package internal

import (
	"testing"
	"time"
)

func TestSettingsPollInterval(t *testing.T) {
	m := NewModule(Config{})
	if got := m.getPollInterval(); got != 15*time.Minute {
		t.Fatalf("default=%v", got)
	}
	if err := m.UpdateSetting("poll_interval", "5m"); err != nil {
		t.Fatal(err)
	}
	if got := m.getPollInterval(); got != 5*time.Minute {
		t.Fatalf("got=%v", got)
	}
	if err := m.UpdateSetting("poll_interval", "nope"); err == nil {
		t.Fatal("expected error")
	}
}
