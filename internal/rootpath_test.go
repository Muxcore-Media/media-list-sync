package internal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func TestAddSourceRootFolderRejectsUnknown(t *testing.T) {
	m := newTestModule(t)
	m.rootsListFn = func(ctx context.Context, mediaKind string) ([]string, error) {
		return []string{"/movies"}, nil
	}
	_, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
		Name:           "Bad",
		Type:           "radarr",
		RootFolderPath: "/elsewhere",
	})
	if err == nil || !strings.Contains(err.Error(), "not a registered root") {
		t.Fatalf("expected reject, got %v", err)
	}
}

func TestAddSourceRootFolderAcceptsRegistered(t *testing.T) {
	m := newTestModule(t)
	m.rootsListFn = func(ctx context.Context, mediaKind string) ([]string, error) {
		return []string{"/movies"}, nil
	}
	added, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
		Name:           "Good",
		Type:           "radarr",
		RootFolderPath: "/movies/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Source.RootFolderPath != "/movies" {
		t.Errorf("path: %q", added.Source.RootFolderPath)
	}
}

func TestAddSourceRootFolderFailsClosedWhenRootsUnavailable(t *testing.T) {
	m := newTestModule(t)
	_, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
		Name:           "Closed",
		Type:           "radarr",
		RootFolderPath: "/unregistered",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("expected fail-closed, got %v", err)
	}
	// Roots lookup error (not just "unavailable") also refuses.
	m.rootsListFn = func(ctx context.Context, mediaKind string) ([]string, error) {
		return nil, errors.New("boom")
	}
	if _, err := m.resolveRootFolderPath(context.Background(), "/movies", ""); err == nil {
		t.Fatal("expected error when roots lookup fails")
	}
	// An empty path (no root requested) is still fine.
	m.rootsListFn = nil
	if got, err := m.resolveRootFolderPath(context.Background(), "", ""); err != nil || got != "" {
		t.Fatalf("empty path: %q %v", got, err)
	}
}

func TestRootFolderEscapesRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "movies")
	sibling := filepath.Join(dir, "movies-private")
	for _, d := range []string{root, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(sibling, link); err != nil {
		t.Fatal(err)
	}
	m := newTestModule(t)
	m.rootsListFn = func(ctx context.Context, mediaKind string) ([]string, error) { return []string{root}, nil }
	for _, p := range []string{
		sibling,                     // sibling sharing a prefix
		root + "/../movies-private", // traversal
		link,                        // symlink inside root pointing outside
		"/etc",                      // unrelated
		"relative/path",             // not absolute
	} {
		if got, err := m.resolveRootFolderPath(context.Background(), p, ""); err == nil {
			t.Errorf("%q accepted as %q", p, got)
		}
	}
	if got, err := m.resolveRootFolderPath(context.Background(), root+"/", ""); err != nil || got != root {
		t.Errorf("registered root: %q %v", got, err)
	}
}
