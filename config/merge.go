package config

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// BuildRequestBody constructs the final CF API request body by merging layers:
//
//	settings.jsonc defaults  (lowest priority)
//	→ site headers for matching domain
//	→ AI config (for /json endpoint only)
//	→ -d body
//	→ CLI flag overrides      (highest priority)
//
// Returns standard JSON bytes ready for WithRequestBody.
func BuildRequestBody(endpoint string, targetURL string, dataBody []byte, flagOverrides map[string]any) ([]byte, error) {
	merged := make(map[string]any)

	// Layer 1: settings defaults
	if settings, err := LoadSettings(); err == nil && settings.Defaults != nil {
		deepMerge(merged, settings.Defaults)
	}

	// Layer 2: site headers for matching domain
	if targetURL != "" {
		if domain := extractDomain(targetURL); domain != "" {
			if headers := SiteHeaders(domain); len(headers) > 0 {
				existing, _ := merged["setExtraHTTPHeaders"].(map[string]any)
				if existing == nil {
					existing = make(map[string]any)
				}
				for k, v := range headers {
					existing[k] = v
				}
				merged["setExtraHTTPHeaders"] = existing
			}
		}
	}

	// Layer 3: AI config (only for /json endpoint)
	if endpoint == "json" {
		if creds, err := LoadCredentials(); err == nil && creds.AI.Model != "" {
			customAI := []map[string]string{{
				"model":         creds.AI.Model,
				"authorization": creds.AI.Authorization,
			}}
			merged["custom_ai"] = customAI
		}
	}

	// Layer 4: -d body
	if dataBody != nil {
		var body map[string]any
		if err := json.Unmarshal(dataBody, &body); err != nil {
			return nil, fmt.Errorf("invalid JSON in -d body: %w", err)
		}
		deepMerge(merged, body)
	}

	// Layer 5: CLI flag overrides (highest priority)
	if len(flagOverrides) > 0 {
		deepMerge(merged, flagOverrides)
	}

	return json.Marshal(merged)
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func deepMerge(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				deepMerge(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}
