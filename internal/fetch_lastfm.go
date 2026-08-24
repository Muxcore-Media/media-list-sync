package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

const lastfmAPIBase = "https://ws.audioscrobbler.com/2.0/"

func (m *Module) fetchLastfm(ctx context.Context, source *listsyncv1.ListSource) ([]syncItem, error) {
	apiKey := strings.TrimSpace(source.GetApiKey())
	if apiKey == "" {
		return nil, fmt.Errorf("lastfm api_key required for source %s", source.GetName())
	}
	spec := parseLastfmListSpec(source)
	switch spec.kind {
	case "chart":
		return m.fetchLastfmChart(ctx, apiKey, spec.limit)
	case "tag":
		return m.fetchLastfmTag(ctx, apiKey, spec.tag, spec.limit)
	case "loved":
		user := spec.user
		if user == "" {
			user = strings.TrimSpace(source.GetUsername())
		}
		if user == "" {
			return nil, fmt.Errorf("lastfm loved list requires user:NAME or username")
		}
		return m.fetchLastfmLoved(ctx, apiKey, user, spec.limit)
	case "library":
		user := spec.user
		if user == "" {
			user = strings.TrimSpace(source.GetUsername())
		}
		if user == "" {
			return nil, fmt.Errorf("lastfm library list requires user:NAME or username")
		}
		return m.fetchLastfmLibrary(ctx, apiKey, user, spec.limit)
	default:
		user := spec.user
		if user == "" {
			user = strings.TrimSpace(source.GetUsername())
		}
		if user == "" {
			user = strings.TrimSpace(source.GetListUrl())
		}
		if user == "" {
			return nil, fmt.Errorf("lastfm top artists requires user:NAME, username, or list_url")
		}
		if strings.Contains(user, ":") {
			parts := strings.SplitN(user, ":", 2)
			user = strings.TrimSpace(parts[1])
		}
		return m.fetchLastfmTopArtists(ctx, apiKey, user, spec.period, spec.limit)
	}
}

type lastfmListSpec struct {
	kind   string
	user   string
	tag    string
	period string
	limit  int
}

func parseLastfmListSpec(source *listsyncv1.ListSource) lastfmListSpec {
	spec := lastfmListSpec{kind: "top", period: "overall", limit: 100}
	raw := strings.TrimSpace(source.GetListUrl())
	if raw == "" {
		return spec
	}
	lower := strings.ToLower(raw)
	switch {
	case lower == "chart" || strings.HasPrefix(lower, "chart:"):
		spec.kind = "chart"
	case strings.HasPrefix(lower, "tag:"):
		spec.kind = "tag"
		spec.tag = strings.TrimSpace(raw[4:])
	case strings.HasPrefix(lower, "loved:"):
		spec.kind = "loved"
		spec.user = strings.TrimSpace(raw[6:])
	case strings.HasPrefix(lower, "library:"):
		spec.kind = "library"
		spec.user = strings.TrimSpace(raw[8:])
	case strings.HasPrefix(lower, "user:"):
		spec.user = strings.TrimSpace(raw[5:])
	case strings.HasPrefix(lower, "period:"):
		spec.period = strings.TrimSpace(raw[7:])
	default:
		spec.user = raw
	}
	if spec.limit <= 0 {
		spec.limit = 100
	}
	if spec.limit > 1000 {
		spec.limit = 1000
	}
	return spec
}

func (m *Module) fetchLastfmTopArtists(ctx context.Context, apiKey, user, period string, limit int) ([]syncItem, error) {
	params := url.Values{}
	params.Set("method", "user.gettopartists")
	params.Set("user", user)
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", clampLastfmLimit(limit)))
	if period != "" {
		params.Set("period", period)
	}
	body, err := m.lastfmGet(ctx, params)
	if err != nil {
		return nil, err
	}
	return parseLastfmArtists(body, fmt.Sprintf("lastfm_top_%s", user))
}

func (m *Module) fetchLastfmLibrary(ctx context.Context, apiKey, user string, limit int) ([]syncItem, error) {
	params := url.Values{}
	params.Set("method", "user.getartists")
	params.Set("user", user)
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", clampLastfmLimit(limit)))
	body, err := m.lastfmGet(ctx, params)
	if err != nil {
		return nil, err
	}
	return parseLastfmArtists(body, fmt.Sprintf("lastfm_library_%s", user))
}

func (m *Module) fetchLastfmChart(ctx context.Context, apiKey string, limit int) ([]syncItem, error) {
	params := url.Values{}
	params.Set("method", "chart.gettopartists")
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", clampLastfmLimit(limit)))
	body, err := m.lastfmGet(ctx, params)
	if err != nil {
		return nil, err
	}
	return parseLastfmArtists(body, "lastfm_chart")
}

func (m *Module) fetchLastfmTag(ctx context.Context, apiKey, tag string, limit int) ([]syncItem, error) {
	if tag == "" {
		return nil, fmt.Errorf("lastfm tag:NAME required")
	}
	params := url.Values{}
	params.Set("method", "tag.gettopartists")
	params.Set("tag", tag)
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", clampLastfmLimit(limit)))
	body, err := m.lastfmGet(ctx, params)
	if err != nil {
		return nil, err
	}
	return parseLastfmArtists(body, fmt.Sprintf("lastfm_tag_%s", tag))
}

func (m *Module) fetchLastfmLoved(ctx context.Context, apiKey, user string, limit int) ([]syncItem, error) {
	params := url.Values{}
	params.Set("method", "user.getlovedtracks")
	params.Set("user", user)
	params.Set("api_key", apiKey)
	params.Set("format", "json")
	params.Set("limit", fmt.Sprintf("%d", clampLastfmLimit(limit)))
	body, err := m.lastfmGet(ctx, params)
	if err != nil {
		return nil, err
	}
	return parseLastfmLovedTracks(body, user)
}

func (m *Module) lastfmGet(ctx context.Context, params url.Values) ([]byte, error) {
	u := lastfmAPIBase + "?" + params.Encode()
	resp, err := m.httpGet(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("last.fm HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var probe struct {
		Message string `json:"message"`
		Error   int    `json:"error"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && probe.Error != 0 {
		return nil, fmt.Errorf("last.fm API error %d: %s", probe.Error, probe.Message)
	}
	return body, nil
}

func parseLastfmArtists(body []byte, prefix string) ([]syncItem, error) {
	var resp struct {
		TopArtists *lastfmArtistBlock `json:"topartists"`
		Artists    *lastfmArtistBlock `json:"artists"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse last.fm artists: %w", err)
	}
	block := resp.TopArtists
	if block == nil {
		block = resp.Artists
	}
	if block == nil {
		return nil, fmt.Errorf("last.fm response missing artists")
	}
	out := make([]syncItem, 0, len(block.Artist))
	for i, a := range block.Artist {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		ext := fmt.Sprintf("%s_%d", prefix, i+1)
		if a.MBID != "" {
			ext = fmt.Sprintf("lastfm_mbid_%s", strings.ToLower(a.MBID))
		}
		out = append(out, syncItem{
			ExternalID: ext, MusicBrainzID: strings.TrimSpace(a.MBID),
			MediaType: "music", Title: name, Action: "lastfm",
		})
	}
	return out, nil
}

func parseLastfmLovedTracks(body []byte, user string) ([]syncItem, error) {
	var resp struct {
		LovedTracks struct {
			Track []struct {
				Name   string `json:"name"`
				Artist struct {
					Name string `json:"name"`
					MBID string `json:"mbid"`
				} `json:"artist"`
			} `json:"track"`
		} `json:"lovedtracks"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse last.fm loved tracks: %w", err)
	}
	seen := map[string]struct{}{}
	out := make([]syncItem, 0)
	for _, tr := range resp.LovedTracks.Track {
		name := strings.TrimSpace(tr.Artist.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(tr.Artist.MBID)
		if key == "" {
			key = strings.ToLower(name)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ext := fmt.Sprintf("lastfm_loved_%s_%s", user, key)
		out = append(out, syncItem{
			ExternalID: ext, MusicBrainzID: strings.TrimSpace(tr.Artist.MBID),
			MediaType: "music", Title: name, Action: "lastfm_loved",
		})
	}
	return out, nil
}

type lastfmArtistBlock struct {
	Artist []struct {
		Name string `json:"name"`
		MBID string `json:"mbid"`
	} `json:"artist"`
}

func clampLastfmLimit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 1000 {
		return 1000
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
