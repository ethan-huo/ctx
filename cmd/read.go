package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cache"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
	"github.com/ethan-huo/ctx/markdown"
)

type ReadCmd struct {
	cfrender.DataFlag
	URL     string `arg:"" help:"URL or local path to read (github://, https://, file://, or path)" optional:""`
	Full    bool   `short:"f" help:"Use Cloudflare Browser Rendering for full JS-rendered content" default:"false"`
	NoCache bool   `help:"Bypass cache, always fetch fresh"`
	TOC     bool   `help:"Show heading outline with section numbers"`
	Section string `short:"s" help:"Section(s) to extract (e.g. 1, 1-3, 1.2,3.1-5.1,6.2)"`
}

const summaryThreshold = 2000

func (c *ReadCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	url := c.URL

	// -d implies -f (CF rendering)
	if dataBody != nil {
		c.Full = true
	}

	// Local file — direct read, no cache, no hints, no summary
	if url != "" {
		if path, ok := localPath(url); ok {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("file not found: %s", path)
			}
			return c.output(path, url, string(data), false)
		}
	}

	// Need a URL from either arg or -d body
	if url == "" && dataBody == nil {
		return fmt.Errorf("URL is required (as argument or in -d body)")
	}

	// Resolve effective URL (may come from -d body when c.URL is empty)
	target := url
	if target == "" && dataBody != nil {
		target = effectiveURL("", dataBody)
	}

	var cacheKey string
	if dataBody != nil {
		cacheKey = cache.Key("markdown", canonicalizeURL(target), string(dataBody))
	} else {
		cacheKey = cache.Key("markdown", canonicalizeURL(target))
	}

	// Try cache
	if !c.NoCache {
		if data, _, ok := cache.Lookup(cacheKey, ".md"); ok {
			return c.output(cache.Path(cacheKey, ".md"), target, string(data), true)
		}
	}

	// Fetch
	content, source, err := c.fetch(url, dataBody)
	if err != nil {
		return err
	}

	// Store clean content (no hints ever appended to stored content)
	_ = cache.Store(cacheKey, []byte(content), ".md", cache.Meta{
		URL:    canonicalizeURL(target),
		Source: source,
	})

	// Hints go to stderr only, per contract Section 4
	// looksIncomplete only applies to source=http (not github, not cloudflare)
	if source == "http" && looksIncomplete(content) {
		fmt.Fprintf(os.Stderr, "Content may be incomplete (JS-rendered page). Re-run with: ctx read -f %s\n", target)
	}

	// Empty content hints (stderr), per source
	if strings.TrimSpace(content) == "" {
		switch source {
		case "http":
			fmt.Fprintf(os.Stderr, "No content returned for %s. Possible causes: authentication required, anti-bot protection, or empty page. Try: ctx read -f %s\n", target, target)
		case "cloudflare":
			fmt.Fprintf(os.Stderr, "No content returned for %s. Possible causes: authentication required (ctx site set %s ...), anti-bot protection, or the page is genuinely empty.\n", target, extractDomainFromURL(target))
		}
	}

	return c.output(cache.Path(cacheKey, ".md"), target, content, true)
}

func extractDomainFromURL(rawURL string) string {
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rest := rawURL[i+3:]
		if j := strings.IndexAny(rest, ":/"); j >= 0 {
			return rest[:j]
		}
		return rest
	}
	return rawURL
}

// fetch dispatches to the right fetcher and returns (content, source, error).
func (c *ReadCmd) fetch(url string, dataBody []byte) (string, string, error) {
	// github://owner/repo@ref/path or github://owner/repo/path
	if strings.HasPrefix(url, "github://") {
		path, ref := parseGitHubScheme(strings.TrimPrefix(url, "github://"))
		content, err := fetchGitHub(path, ref)
		return content, "github", err
	}

	// https://github.com/owner/repo/blob/branch/path
	if strings.Contains(url, "github.com") {
		if path, ref, ok := parseGitHubBlobURL(url); ok {
			content, err := fetchGitHub(path, ref)
			if err != nil && strings.Contains(err.Error(), "GitHub API 404") {
				// The 404 might be a real not-found, or it might be a slash-ref
				// misparsing (e.g. "feature/auth" parsed as ref="feature").
				// Return the real error; add a stderr note about possible ambiguity.
				parts := strings.SplitN(path, "/", 3)
				if len(parts) >= 3 {
					fmt.Fprintf(os.Stderr,
						"Note: if the branch name contains '/', ref %q may have been parsed incorrectly.\n"+
							"Try: ctx read github://%s/%s@<ref>/%s\n",
						ref, parts[0], parts[1], parts[2])
				}
			}
			return content, "github", err
		}
	}

	// -f flag or -d provided: Cloudflare Browser Rendering
	if c.Full {
		content, err := fetchCloudflare(url, dataBody)
		return content, "cloudflare", err
	}

	// Default: HTTP with markdown negotiation, CF fallback for HTML
	content, err := fetchHTTP(url)
	if err != nil {
		return "", "", err
	}
	if content != "" {
		return content, "http", nil
	}

	// HTML response — fallback to Cloudflare Browser Rendering
	fmt.Fprintf(os.Stderr, "HTML response, rendering via Cloudflare...\n")
	content, err = fetchCloudflare(url, dataBody)
	return content, "cloudflare", err
}

// output handles --toc, -s, and default (with structural summary for long docs).
// contentPath is the cache file path (or local file path).
// target is the effective URL/path for user-facing messages and summary navigation hints.
// allowSummary controls whether long documents get a structural summary (false for local files).
func (c *ReadCmd) output(contentPath, target, content string, allowSummary bool) error {
	source := []byte(content)

	if c.TOC {
		headings := markdown.ParseHeadings(source)
		fmt.Print(markdown.FormatTOC(source, headings))
		return nil
	}

	if c.Section != "" {
		headings := markdown.ParseHeadings(source)
		ranges, err := markdown.ParseSectionExpr(c.Section)
		if err != nil {
			return err
		}
		matched, err := markdown.ExpandRanges(headings, ranges)
		if err != nil {
			return err
		}
		if len(matched) == 0 {
			return fmt.Errorf("no sections matched %q — use: ctx read %s --toc", c.Section, target)
		}
		for i, h := range matched {
			if i > 0 {
				fmt.Println()
			}
			fmt.Print(markdown.ExtractSection(source, h))
		}
		return nil
	}

	// Default mode: full content for short docs or local files; structural summary for long remote docs
	lines := strings.Count(content, "\n") + 1
	if !allowSummary || lines <= summaryThreshold {
		fmt.Print(content)
		return nil
	}

	// Long remote document → structural summary
	headings := markdown.ParseHeadings(source)
	if len(headings) > 0 {
		fmt.Print(markdown.FormatSummary(source, headings, target, contentPath))
	} else {
		fmt.Print(markdown.FormatLineSummary(source, contentPath))
	}
	return nil
}

// canonicalizeURL normalizes GitHub blob URLs to github:// form for cache key consistency.
// When a ref is present, the format is github://owner/repo@ref/path.
func canonicalizeURL(url string) string {
	if strings.Contains(url, "github.com") {
		if path, ref, ok := parseGitHubBlobURL(url); ok {
			return formatGitHubScheme(path, ref)
		}
	}
	return url
}

// formatGitHubScheme builds a github:// URI, embedding @ref in the owner/repo segment when non-empty.
func formatGitHubScheme(path, ref string) string {
	if ref == "" {
		return "github://" + path
	}
	// path is "owner/repo/file/path" — inject @ref after repo
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 {
		return "github://" + path
	}
	return fmt.Sprintf("github://%s/%s@%s/%s", parts[0], parts[1], ref, parts[2])
}

// parseGitHubScheme extracts the plain owner/repo/path and optional ref from
// a github:// URI body like "owner/repo@ref/path" or "owner/repo/path".
// Supports URL-encoded refs: owner/repo@feature%2Fauth/path → ref="feature/auth"
func parseGitHubScheme(raw string) (path, ref string) {
	// Split into at most 3 parts: owner, repoMaybeRef, filePath
	parts := strings.SplitN(raw, "/", 3)
	if len(parts) < 2 {
		return raw, ""
	}
	repo := parts[1]
	if at := strings.IndexByte(repo, '@'); at >= 0 {
		ref = repo[at+1:]
		// URL-decode ref to support %2F for slash-containing branch names
		if decoded, err := url.PathUnescape(ref); err == nil {
			ref = decoded
		}
		parts[1] = repo[:at]
	}
	return strings.Join(parts, "/"), ref
}

// --- Fetch functions (pure: return content, no stdout side effects) ---

func fetchGitHub(path, ref string) (string, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid github path: %s (expected owner/repo/path)", path)
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", parts[0], parts[1], parts[2])
	if ref != "" {
		apiURL += "?ref=" + ref
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if token := ghToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 403 && strings.Contains(string(body), "rate limit") {
			return "", fmt.Errorf("GitHub API rate limited. Set GITHUB_TOKEN or run: gh auth login")
		}
		return "", fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(
			strings.ReplaceAll(result.Content, "\n", ""),
		)
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}

	return result.Content, nil
}

func fetchHTTP(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Accept",
		"text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, "+
			"application/json;q=0.7, application/xml;q=0.6, text/html;q=0.5")
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	ct := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", err
	}

	// Accept text-like content directly (no CF fallback needed)
	if isDirectlyReadable(ct) {
		return string(body), nil
	}

	// HTML / unknown → signal caller to use CF fallback
	return "", nil
}

func fetchCloudflare(targetURL string, dataBody []byte) (string, error) {
	overrides := make(map[string]any)
	if targetURL != "" {
		overrides["url"] = targetURL
	}
	body, err := config.BuildRequestBody("markdown", targetURL, dataBody, overrides)
	if err != nil {
		return "", err
	}
	c, err := cfrender.New()
	if err != nil {
		return "", err
	}
	return c.Markdown(context.Background(), targetURL, body)
}

// localPath resolves file://, absolute, relative, and ~ paths.
// Returns the resolved absolute path and true if the input is a local path.
func localPath(url string) (string, bool) {
	var p string
	switch {
	case strings.HasPrefix(url, "file://"):
		p = strings.TrimPrefix(url, "file://")
	case strings.HasPrefix(url, "/"):
		p = url
	case strings.HasPrefix(url, "./"), strings.HasPrefix(url, "../"):
		p = url
	case strings.HasPrefix(url, "~/"):
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, url[2:])
	default:
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p, true
	}
	return abs, true
}

// --- Helpers ---

func looksIncomplete(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 500 {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, sig := range []string{"enable javascript", "loading...", "<noscript", "this page requires javascript"} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// isDirectlyReadable returns true for content types that can be used as-is
// without browser rendering: markdown, plain text, JSON, XML, YAML, etc.
func isDirectlyReadable(ct string) bool {
	ct = strings.ToLower(ct)
	for _, prefix := range []string{
		"text/markdown", "text/x-markdown",
		"text/plain",
		"application/json", "text/json",
		"application/xml", "text/xml",
		"application/yaml", "text/yaml", "application/x-yaml",
	} {
		if strings.Contains(ct, prefix) {
			return true
		}
	}
	// Accept any text/* subtype except text/html
	if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "text/html") {
		return true
	}
	return false
}

func parseGitHubBlobURL(rawURL string) (path string, ref string, ok bool) {
	trimmed := strings.TrimPrefix(rawURL, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	parts := strings.SplitN(trimmed, "/blob/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	repo := parts[0]
	rest := parts[1]
	idx := strings.IndexByte(rest, '/')
	if idx < 0 {
		return "", "", false
	}
	ref = rest[:idx]
	rest = rest[idx+1:]
	if idx := strings.IndexByte(rest, '?'); idx >= 0 {
		rest = rest[:idx]
	}
	if idx := strings.IndexByte(rest, '#'); idx >= 0 {
		rest = rest[:idx]
	}
	return repo + "/" + rest, ref, true
}

func ghToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

var httpClient = &http.Client{Timeout: 30 * time.Second}
