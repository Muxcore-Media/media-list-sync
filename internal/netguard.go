package internal

import (
	"fmt"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

var (
	// integrationOpts: Radarr/Sonarr/Jellyfin are admin-configured services,
	// usually on the LAN or loopback. Metadata/link-local stay blocked.
	integrationOpts = netguard.Options{AllowPrivate: true, AllowLoopback: true, Timeout: 30 * time.Second}
	// externalOpts: fixed third-party hosts (Plex, Trakt); never private.
	externalOpts = netguard.Options{RequireHTTPS: true, Timeout: 30 * time.Second}
	// imdbOpts: IMDb list exports are pinned to imdb.com.
	imdbOpts = netguard.Options{
		RequireHTTPS: true,
		AllowedHosts: []string{"imdb.com", "*.imdb.com"},
		Timeout:      30 * time.Second,
	}
)

// validateIntegrationURL checks an admin-configured service base URL.
func validateIntegrationURL(raw string) error {
	if err := netguard.ValidateURL(raw, netguard.Integration, integrationOpts); err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	return nil
}

// validateIMDbURL ensures an IMDb list URL points at imdb.com over https.
func validateIMDbURL(raw string) error {
	if err := netguard.ValidateURL(raw, netguard.UserURL, imdbOpts); err != nil {
		return fmt.Errorf("invalid imdb list_url: %w", err)
	}
	return nil
}

// validateSourceURLs rejects unsafe URLs when a source is added or updated.
func validateSourceURLs(sourceType, baseURL, listURL string) error {
	if baseURL != "" {
		if err := validateIntegrationURL(baseURL); err != nil {
			return err
		}
	}
	if strings.EqualFold(sourceType, "imdb") && listURL != "" {
		if err := validateIMDbURL(listURL); err != nil {
			return err
		}
	}
	return nil
}
