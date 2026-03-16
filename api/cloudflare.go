package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type CFCredentials struct {
	AccountID string `json:"account_id"`
	APIToken  string `json:"api_token"`
}

func cfCredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "docs7", "credentials.json")
}

func LoadCFCredentials() (*CFCredentials, error) {
	data, err := os.ReadFile(cfCredentialsPath())
	if err != nil {
		return nil, err
	}
	var c CFCredentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.AccountID == "" || c.APIToken == "" {
		return nil, fmt.Errorf("incomplete cloudflare credentials")
	}
	return &c, nil
}

func SaveCFCredentials(c *CFCredentials) error {
	dir := filepath.Dir(cfCredentialsPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfCredentialsPath(), data, 0o600)
}

func ClearCFCredentials() error {
	return os.Remove(cfCredentialsPath())
}

// ValidateCFToken verifies the token has Browser Rendering permission
// by making a real API call with a known URL.
func ValidateCFToken(accountID, token string) error {
	_, err := FetchMarkdownCF(accountID, token, "https://example.com")
	if err != nil {
		return fmt.Errorf("token validation failed: %w", err)
	}
	return nil
}

// FetchMarkdownCF uses Cloudflare Browser Rendering to convert a URL to markdown.
func FetchMarkdownCF(accountID, token, targetURL string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/browser-rendering/markdown",
		accountID,
	)
	payload, _ := json.Marshal(map[string]any{
		"url":         targetURL,
		"gotoOptions": map[string]any{"waitUntil": "networkidle0"},
	})

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", err
	}

	var result struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("invalid response: %s", string(body))
	}

	if !result.Success {
		if len(result.Errors) > 0 {
			return "", fmt.Errorf("cloudflare API error %d: %s", result.Errors[0].Code, result.Errors[0].Message)
		}
		return "", fmt.Errorf("cloudflare API error: %s", string(body))
	}

	var markdown string
	if err := json.Unmarshal(result.Result, &markdown); err != nil {
		return "", fmt.Errorf("unexpected result format: %s", string(result.Result))
	}
	return markdown, nil
}
