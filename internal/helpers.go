package internal

import (
	"context"
	"math"
	"net/http"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

func clampInt32FromInt(v int) int32 {
	return clampInt32(int64(v))
}

func (m *Module) httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	return m.httpCli.Do(req)
}
