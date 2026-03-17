package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/tidwall/jsonc"
)

// Settings is the behavioral config file (~/.config/ctx/settings.jsonc).
type Settings struct {
	Defaults map[string]any `json:"defaults,omitempty"`
}

func settingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ctx", "settings.jsonc")
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
