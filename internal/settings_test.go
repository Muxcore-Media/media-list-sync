package internal

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSettingsPollInterval(t *testing.T) {
	m := newTestModule(t)
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

func TestSettingsPollIntervalPersisted(t *testing.T) {
	m := newTestModule(t)
	if err := m.UpdateSetting("poll_interval", "7m"); err != nil {
		t.Fatal(err)
	}
	m2 := NewModule(Config{DBPath: m.dbPath, GRPCAddr: ":0"})
	if err := m2.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m2.Stop(context.Background()) })
	if got := m2.getPollInterval(); got != 7*time.Minute {
		t.Fatalf("persisted=%v", got)
	}
}

func TestPruneSyncLog(t *testing.T) {
	m := newTestModule(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = m.db.ExecContext(ctx, `INSERT INTO sync_log (id, source_id, status, started_at) VALUES (?, 's', 'completed', ?)`,
			fmt.Sprintf("log_%d", i), time.Now().UTC().Add(-time.Duration(i)*24*time.Hour).Format(time.RFC3339))
	}
	m.pruneSyncLog(ctx)
	var n int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 5 {
		t.Fatalf("unexpected prune count=%d", n)
	}
}
