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

	// Layer 1.5: Default DOM cleanup for markdown endpoint.
	// Removes common navigation noise (sidebar, header, footer, nav) so the
	// extracted markdown contains mostly page content.
	// This runs after settings defaults (which may add their own scripts) and
	// appends rather than replaces. Can be overridden by -d body or CLI flags.
	if endpoint == "markdown" {
		injectDefaultCleanup(merged)
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

// injectDefaultCleanup appends a script that isolates the main content block
// using a text-density heuristic: find the container with the highest
// text-to-link ratio, then replace the body with just that element.
// This works on sites with non-semantic HTML (no <nav>/<aside>/standard classes)
// by focusing on content density rather than tag names.
func injectDefaultCleanup(merged map[string]any) {
	script := `(function(){` +
		// Phase 1: try to isolate the densest content block
		`let best=null,bestScore=0;` +
		`document.querySelectorAll("div,section,article,main").forEach(el=>{` +
		`const t=el.innerText||"";` +
		`const links=el.querySelectorAll("a").length;` +
		`const score=t.length/(links+1);` +
		`if(t.length>200&&score>bestScore){bestScore=score;best=el}` +
		`});` +
		`if(best){document.body.innerHTML=best.outerHTML;return}` +
		// Phase 2: fallback — remove known junk by selector
		`document.querySelectorAll("nav,header,footer,aside,` +
		`[role=navigation],[role=banner],[role=contentinfo],` +
		`[class*=sidebar],[class*=menu],[class*=nav-],[class*=footer],` +
		`[class*=header],[id*=sidebar],[id*=menu],[id*=nav]` +
		`").forEach(e=>e.remove())` +
		`})()`

	tag := map[string]any{"content": script}

	if existing, ok := merged["addScriptTag"].([]any); ok {
		merged["addScriptTag"] = append(existing, tag)
	} else {
		merged["addScriptTag"] = []any{tag}
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
