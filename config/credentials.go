package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Credentials is the unified credentials file (~/.config/ctx/credentials.yaml).
type Credentials struct {
	Cloudflare CloudflareCreds `yaml:"cloudflare,omitempty"`
	Ctx7       Ctx7Creds      `yaml:"ctx7,omitempty"`
	AI         AICreds        `yaml:"ai,omitempty"`
	Sites      map[string]Site `yaml:"sites,omitempty"`
}

type CloudflareCreds struct {
	AccountID string `yaml:"account_id,omitempty"`
	APIToken  string `yaml:"api_token,omitempty"`
}

type Ctx7Creds struct {
	AccessToken  string `yaml:"access_token,omitempty"`
	RefreshToken string `yaml:"refresh_token,omitempty"`
	TokenType    string `yaml:"token_type,omitempty"`
	ExpiresIn    int64  `yaml:"expires_in,omitempty"`
	ExpiresAt    int64  `yaml:"expires_at,omitempty"`
	Scope        string `yaml:"scope,omitempty"`
}

type AICreds struct {
	Model         string `yaml:"model,omitempty"`
	Authorization string `yaml:"authorization,omitempty"`
}

// Site holds per-domain headers that get auto-injected into CF requests.
type Site struct {
	Headers map[string]string `yaml:"headers,omitempty"`
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ctx", "credentials.yaml")
}

func LoadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return migrateOldCredentials()
		}
		return nil, err
	}
	var c Credentials
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid credentials.yaml: %w", err)
	}
	return &c, nil
}

func SaveCredentials(c *Credentials) error {
	dir := filepath.Dir(credentialsPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(credentialsPath(), data, 0o600)
}

// UpdateCredentials loads, applies a mutation, and saves.
func UpdateCredentials(fn func(*Credentials)) error {
	c, err := LoadCredentials()
	if err != nil {
		c = &Credentials{}
	}
	fn(c)
	return SaveCredentials(c)
}

// --- Backward compatibility: migrate old JSON files ---

func migrateOldCredentials() (*Credentials, error) {
	c := &Credentials{}
	migrated := false

	if cf, err := loadOldCloudflareJSON(); err == nil {
		c.Cloudflare = *cf
		migrated = true
	}
	if ctx7, err := loadOldCtx7JSON(); err == nil {
		c.Ctx7 = *ctx7
		migrated = true
	}

	if migrated {
		// Save as new format (don't delete old files — user can do that manually)
		_ = SaveCredentials(c)
	}

	return c, nil
}

func loadOldCloudflareJSON() (*CloudflareCreds, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".config", "ctx", "cloudflare.json"))
	if err != nil {
		return nil, err
	}
	// Old format: {"account_id": "...", "api_token": "..."}
	// YAML-compatible enough to parse as YAML
	var c CloudflareCreds
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.AccountID == "" || c.APIToken == "" {
		return nil, fmt.Errorf("incomplete")
	}
	return &c, nil
}

func loadOldCtx7JSON() (*Ctx7Creds, error) {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".config", "ctx", "ctx7.json"))
	if err != nil {
		return nil, err
	}
	var c Ctx7Creds
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// --- Convenience accessors (used by api/ package as bridge) ---

func LoadCloudflare() (accountID, apiToken string, err error) {
	c, err := LoadCredentials()
	if err != nil {
		return "", "", err
	}
	if c.Cloudflare.AccountID == "" || c.Cloudflare.APIToken == "" {
		return "", "", fmt.Errorf("cloudflare not configured")
	}
	return c.Cloudflare.AccountID, c.Cloudflare.APIToken, nil
}

func LoadCtx7Token() (accessToken string, expiresAt int64, err error) {
	c, err := LoadCredentials()
	if err != nil {
		return "", 0, err
	}
	return c.Ctx7.AccessToken, c.Ctx7.ExpiresAt, nil
}

func IsCtx7Expired(expiresAt int64) bool {
	if expiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli() > expiresAt-60000
}

// SiteHeaders returns the configured headers for a domain, or nil.
func SiteHeaders(domain string) map[string]string {
	c, err := LoadCredentials()
	if err != nil {
		return nil
	}
	if s, ok := c.Sites[domain]; ok {
		return s.Headers
	}
	return nil
}
