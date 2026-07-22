package internal

import (
	"context"
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

func TestAddSourceRootFolderSoftUnavailable(t *testing.T) {
	m := newTestModule(t)
	added, err := m.AddSource(context.Background(), &listsyncv1.AddSourceRequest{
		Name:           "Soft",
		Type:           "radarr",
		RootFolderPath: "/unregistered",
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Source.RootFolderPath != "/unregistered" {
		t.Errorf("path: %q", added.Source.RootFolderPath)
	}
}
