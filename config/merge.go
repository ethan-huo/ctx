package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
)

//go:embed cleanup.js
var cleanupScript string

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

	// Resolve effective URL for site header lookup.
	// Commands like links/screenshot/json/scrape pass the URL only via -d body,
	// leaving targetURL empty and silently skipping per-domain headers.
	effectiveURL := targetURL
	if effectiveURL == "" {
		if u, ok := flagOverrides["url"].(string); ok {
			effectiveURL = u
		}
	}
	if effectiveURL == "" && dataBody != nil {
		var peek map[string]any
		if err := json.Unmarshal(dataBody, &peek); err == nil {
			if u, ok := peek["url"].(string); ok {
				effectiveURL = u
			}
		}
	}

	// Layer 1: settings defaults
	if settings, err := LoadSettings(); err == nil && settings.Defaults != nil {
		deepMerge(merged, settings.Defaults)
	}

	// Layer 1.5: Sensible default viewport.
	// Puppeteer's 800×600 is too small for modern pages. 1440×900 is a
	// reasonable desktop viewport that shows enough content per screen.
	// Lowest priority — overridden by settings, -d body, or CLI flags.
	injectDefaultViewport(merged)

	// Layer 1.6: Default DOM cleanup for markdown endpoint.
	// Three-phase script: semantic container → guarded density → subtractive.
	// See cleanup.js for the full strategy and rationale.
	if endpoint == "markdown" {
		injectDefaultCleanup(merged)
	}

	// Layer 1.6: Default gotoOptions for endpoints that navigate.
	// Wait for network idle so SPA/lazy-loaded content has time to render.
	switch endpoint {
	case "markdown", "screenshot", "scrape", "links", "json":
		injectDefaultGotoOptions(merged)
	}

	// Layer 2: site headers for matching domain
	if effectiveURL != "" {
		if domain := extractDomain(effectiveURL); domain != "" {
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

// injectDefaultCleanup appends the embedded cleanup.js script to addScriptTag.
func injectDefaultCleanup(merged map[string]any) {
	tag := map[string]any{"content": cleanupScript}

	if existing, ok := merged["addScriptTag"].([]any); ok {
		merged["addScriptTag"] = append(existing, tag)
	} else {
		merged["addScriptTag"] = []any{tag}
	}
}

// injectDefaultViewport sets a 1440×900 viewport unless one is already present.
func injectDefaultViewport(merged map[string]any) {
	if _, ok := merged["viewport"]; ok {
		return
	}
	merged["viewport"] = map[string]any{
		"width":  1440,
		"height": 900,
	}
}

// injectDefaultGotoOptions sets waitUntil: "networkidle2" unless gotoOptions is already present.
func injectDefaultGotoOptions(merged map[string]any) {
	if _, ok := merged["gotoOptions"]; ok {
		return
	}
	merged["gotoOptions"] = map[string]any{
		"waitUntil": "networkidle2",
	}
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
