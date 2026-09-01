package internal

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	automationv1 "github.com/Muxcore-Media/contracts-automation/muxcore/automation/v1"
	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
	musicv1 "github.com/Muxcore-Media/media-music/proto/gen/muxcore/music/v1"
	musicbrainzv1 "github.com/Muxcore-Media/metadata-musicbrainz/proto/musicbrainzv1"
)

func (m *Module) ensureMusic(ctx context.Context) error {
	m.mu.RLock()
	if m.musicClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	addr, err := m.findCapabilityAddr(ctx, "media.library.music")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial music: %w", err)
	}
	m.mu.Lock()
	m.musicConn = conn
	m.musicClient = musicv1.NewMusicManagementServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) ensureMusicBrainz(ctx context.Context) error {
	m.mu.RLock()
	if m.musicBrainzClient != nil {
		m.mu.RUnlock()
		return nil
	}
	m.mu.RUnlock()

	addr, err := m.findCapabilityAddr(ctx, "metadata.musicbrainz")
	if err != nil {
		addr, err = m.findCapabilityAddr(ctx, "metadata.music")
	}
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial metadata-musicbrainz: %w", err)
	}
	m.mu.Lock()
	m.musicBrainzConn = conn
	m.musicBrainzClient = musicbrainzv1.NewMusicBrainzServiceClient(conn)
	m.mu.Unlock()
	return nil
}

func (m *Module) resolveMusicBrainz(ctx context.Context, item syncItem) (syncItem, bool) {
	if strings.TrimSpace(item.MusicBrainzID) != "" {
		return item, true
	}
	name := strings.TrimSpace(item.Title)
	if name == "" {
		return item, false
	}
	if err := m.ensureMusicBrainz(ctx); err != nil {
		return item, false
	}
	m.mu.RLock()
	cli := m.musicBrainzClient
	m.mu.RUnlock()
	resp, err := cli.SearchArtist(ctx, &musicbrainzv1.SearchArtistRequest{Query: name, Limit: 5})
	if err != nil || len(resp.GetArtists()) == 0 {
		return item, false
	}
	best := resp.GetArtists()[0]
	item.MusicBrainzID = best.GetId()
	if item.Title == "" {
		item.Title = best.GetName()
	}
	return item, true
}

func (m *Module) addMusicArtist(ctx context.Context, source *listsyncv1.ListSource, item syncItem) (string, error) {
	if err := m.ensureMusic(ctx); err != nil {
		return "", err
	}
	m.mu.RLock()
	mc := m.musicClient
	m.mu.RUnlock()
	resp, err := mc.AddArtist(ctx, &musicv1.AddArtistRequest{
		Name:             item.Title,
		MusicbrainzId:    item.MusicBrainzID,
		Monitored:        musicMonitoredFromSource(source),
		QualityProfileId: source.GetQualityProfileId(),
		RootFolderPath:   source.GetRootFolderPath(),
	})
	if err != nil {
		return "", err
	}
	artistID := resp.GetArtist().GetId()
	_, _ = mc.RefreshMetadata(ctx, &musicv1.RefreshMetadataRequest{ArtistId: artistID})
	return artistID, nil
}

func (m *Module) queueMusicArtistSearch(ctx context.Context, artistID string, item syncItem) {
	if artistID == "" {
		return
	}
	if err := m.ensureAutomation(ctx); err != nil {
		return
	}
	m.mu.RLock()
	ac := m.automationClient
	m.mu.RUnlock()
	_, _ = ac.AddToQueue(ctx, &automationv1.AddToQueueRequest{
		ItemType: "music", ItemId: artistID, Title: item.Title,
	})
}
