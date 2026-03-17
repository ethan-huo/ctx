package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tidwall/jsonc"
)

// Settings is the behavioral config file (~/.config/ctx/settings.jsonc).
type Settings struct {
	Defaults map[string]any `json:"defaults,omitempty"`
	Cache    CacheSettings  `json:"cache,omitempty"`
}

type CacheSettings struct {
	TTL string `json:"ttl,omitempty"` // e.g. "1h", "30m", "4h"
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ctx", "settings.jsonc")
}

// CacheTTL returns the configured cache TTL, or the given fallback.
func CacheTTL(fallback time.Duration) time.Duration {
	s, err := LoadSettings()
	if err != nil || s.Cache.TTL == "" {
		return fallback
	}
	d, err := time.ParseDuration(s.Cache.TTL)
	if err != nil {
		return fallback
	}
	return d
}

func LoadSettings() (*Settings, error) {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Settings{}, nil
		}
		return nil, err
	}
	// Strip JSONC comments → standard JSON
	clean := jsonc.ToJSON(data)
	var s Settings
	if err := json.Unmarshal(clean, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
