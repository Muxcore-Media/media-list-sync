package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	listsyncv1 "github.com/Muxcore-Media/media-list-sync/proto/listsyncv1"
)

func (m *Module) triggerArrImportListSync(source *listsyncv1.ListSource) error {
	baseURL := strings.TrimRight(strings.TrimSpace(source.GetBaseUrl()), "/")
	apiKey := strings.TrimSpace(source.GetApiKey())
	if baseURL == "" || apiKey == "" {
		return fmt.Errorf("base_url and api_key required")
	}
	payload, _ := json.Marshal(map[string]string{"name": "ImportListSync"})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v3/command", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("arr ImportListSync command %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	var cmd struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &cmd); err != nil || cmd.ID <= 0 {
		return nil
	}
	return m.waitArrCommand(baseURL, apiKey, cmd.ID)
}

func (m *Module) waitArrCommand(baseURL, apiKey string, commandID int) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v3/command/%d", baseURL, commandID), nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Accept", "application/json")
		resp, err := m.httpCli.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("arr command status %d", resp.StatusCode)
		}
		var status struct {
			Status string `json:"status"`
			Error  string `json:"message"`
		}
		_ = json.Unmarshal(body, &status)
		switch strings.ToLower(status.Status) {
		case "completed":
			return nil
		case "failed":
			if status.Error != "" {
				return fmt.Errorf("arr command failed: %s", status.Error)
			}
			return fmt.Errorf("arr command failed")
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("arr ImportListSync timed out")
}

func (m *Module) refreshArrImportListCache(source *listsyncv1.ListSource) {
	if err := m.triggerArrImportListSync(source); err != nil {
		slog.Debug("arr ImportListSync before fetch failed", "source", source.GetName(), "error", err)
	}
}
