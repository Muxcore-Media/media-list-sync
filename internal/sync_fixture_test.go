package internal

import (
	"context"
	"net"
	"testing"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type fixtureMovies struct {
	mgmntv1.UnimplementedMovieManagementServiceServer
	adds []int32
}

func (f *fixtureMovies) AddMovie(_ context.Context, req *mgmntv1.AddMovieRequest) (*mgmntv1.AddMovieResponse, error) {
	f.adds = append(f.adds, req.GetTmdbId())
	return &mgmntv1.AddMovieResponse{MovieId: "mv_fixture"}, nil
}

type fixtureTV struct {
	tvmgmtv1.UnimplementedTvManagementServiceServer
	adds []int32
}

func (f *fixtureTV) AddTVShow(_ context.Context, req *tvmgmtv1.AddTVShowRequest) (*tvmgmtv1.AddTVShowResponse, error) {
	f.adds = append(f.adds, req.GetTmdbId())
	return &tvmgmtv1.AddTVShowResponse{SeriesId: "tv_fixture"}, nil
}

func dialBuf(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})
	conn, err := grpc.NewClient("passthrough:///buf",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestFixtureSyncIntoMoviesAndTV(t *testing.T) {
	movies := &fixtureMovies{}
	tv := &fixtureTV{}
	mConn := dialBuf(t, func(s *grpc.Server) { mgmntv1.RegisterMovieManagementServiceServer(s, movies) })
	tConn := dialBuf(t, func(s *grpc.Server) { tvmgmtv1.RegisterTvManagementServiceServer(s, tv) })

	m := newTestModule(t)
	m.moviesClient = mgmntv1.NewMovieManagementServiceClient(mConn)
	m.tvClient = tvmgmtv1.NewTvManagementServiceClient(tConn)

	src := &listsyncv1.ListSource{
		Id: "src_fixture", Name: "Fixture", Type: "imdb",
		QualityProfileId: "qp1", RootFolderPath: "/media",
	}
	items := []syncItem{
		{ExternalID: "tt0137523", TMDBID: 550, MediaType: "movie", Title: "Fight Club", Year: 1999, Action: "watchlist"},
		{ExternalID: "tt0903747", TMDBID: 1396, MediaType: "tv", Title: "Breaking Bad", Year: 2008, Action: "watchlist"},
	}
	if n := m.storeItems(context.Background(), src.Id, items); n != 2 {
		t.Fatalf("store=%d", n)
	}
	m.addItemsToLibrary(context.Background(), src, items)

	if len(movies.adds) != 1 || movies.adds[0] != 550 {
		t.Fatalf("movies adds: %v", movies.adds)
	}
	if len(tv.adds) != 1 || tv.adds[0] != 1396 {
		t.Fatalf("tv adds: %v", tv.adds)
	}
	if m.syncItemStatus(context.Background(), src.Id, "tt0137523") != "added" {
		t.Fatal("movie not marked added")
	}
	if m.syncItemStatus(context.Background(), src.Id, "tt0903747") != "added" {
		t.Fatal("tv not marked added")
	}
}
