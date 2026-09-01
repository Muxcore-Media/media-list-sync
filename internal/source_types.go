package internal

import "fmt"

var validSourceTypes = map[string]struct{}{
	"trakt":              {},
	"imdb":               {},
	"plex":               {},
	"jellyfin":           {},
	"radarr":             {},
	"sonarr":             {},
	"radarr_importlist":  {},
	"sonarr_importlist":  {},
	"trakt_list":         {},
	"stevenlu":           {},
	"tmdb_list":          {},
	"rss":                {},
	"watcharr":           {},
	"lastfm":             {},
}

func validateSourceType(typ string) error {
	if _, ok := validSourceTypes[typ]; !ok {
		return fmt.Errorf("unknown source type %q", typ)
	}
	return nil
}
