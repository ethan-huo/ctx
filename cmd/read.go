package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cache"
	"github.com/ethan-huo/ctx/markdown"
)

type ReadCmd struct {
	URL     string `arg:"" help:"URL or local path to read (github://, https://, file://, or path)"`
	Full    bool   `short:"f" help:"Use Cloudflare Browser Rendering for full JS-rendered content" default:"false"`
	NoCache bool   `help:"Bypass cache, always fetch fresh"`
	TOC     bool   `help:"Show heading outline with section numbers"`
	Section string `short:"s" help:"Section(s) to extract (e.g. 1, 1-3, 1.2,3.1-5.1,6.2)"`
}

const (
	truncateThreshold = 2000
	truncateOutput    = 1000
)

func (c *ReadCmd) Run(_ *api.Client) error {
	url := c.URL

	// Local file — direct read, no cache
	if path, ok := localPath(url); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return c.output(path, string(data))
	}

	cacheURL := canonicalizeURL(url)

	// Try cache
	if !c.NoCache {
		if content, _, ok := cache.Lookup(cacheURL); ok {
			return c.output(cache.ContentPath(cacheURL), content)
		}
	}

	// Fetch
	content, source, err := c.fetch(url)
	if err != nil {
		return err
	}

	incomplete := looksIncomplete(content)

	// Store clean content before appending any hints
	_ = cache.Store(cacheURL, content, source)

	if incomplete {
		content += fmt.Sprintf(
			"\n---\nContent may be incomplete (JS-rendered page). Use `ctx read -f %s` for full rendering.\n", url)
	}

	return c.output(cache.ContentPath(cacheURL), content)
}

// fetch dispatches to the right fetcher and returns (content, source, error).
func (c *ReadCmd) fetch(url string) (string, string, error) {
	// github://owner/repo/path
	if strings.HasPrefix(url, "github://") {
		content, err := fetchGitHub(strings.TrimPrefix(url, "github://"))
		return content, "github", err
	}

	// https://github.com/owner/repo/blob/branch/path
	if strings.Contains(url, "github.com") {
		if path, ok := parseGitHubBlobURL(url); ok {
			content, err := fetchGitHub(path)
			return content, "github", err
		}
	}

	// -f flag: Cloudflare Browser Rendering
	if c.Full {
		content, err := fetchCloudflare(url)
		return content, "cloudflare", err
	}

	// Default: HTTP with markdown negotiation + Jina fallback
	content, err := fetchHTTP(url)
	return content, "http", err
}

// output handles --toc, -s, and default (with truncation).
// contentPath is the file path shown in truncation hints (cache path or local file path).
func (c *ReadCmd) output(contentPath, content string) error {
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
			return fmt.Errorf("no sections matched %q", c.Section)
		}
		for i, h := range matched {
			if i > 0 {
				fmt.Println()
			}
			fmt.Print(markdown.ExtractSection(source, h))
		}
		return nil
	}

	// Default output with truncation
	lines := strings.Split(content, "\n")
	if len(lines) > truncateThreshold {
		fmt.Println(strings.Join(lines[:truncateOutput], "\n"))
		fmt.Printf("\n---\nDocument truncated (%d lines). Full content: %s\n"+
			"Use --toc to see outline, or -s <number> to read a section.\n",
			len(lines), contentPath)
		return nil
	}

	fmt.Print(content)
	return nil
}

// canonicalizeURL normalizes GitHub blob URLs to github:// form for cache key consistency.
func canonicalizeURL(url string) string {
	if strings.Contains(url, "github.com") {
		if path, ok := parseGitHubBlobURL(url); ok {
			return "github://" + path
		}
	}
	return url
}

// --- Fetch functions (pure: return content, no stdout side effects) ---

func fetchGitHub(path string) (string, error) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid github path: %s (expected owner/repo/path)", path)
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", parts[0], parts[1], parts[2])

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
		"text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1")
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

	if isMarkdownContentType(ct) || isPlainText(ct) {
		return string(body), nil
	}

	// HTML response — fallback to Jina Reader
	return fetchJina(url)
}

func fetchCloudflare(url string) (string, error) {
	creds, err := api.LoadCFCredentials()
	if err != nil {
		return "", fmt.Errorf("cloudflare not configured — run `ctx auth cloudflare` first")
	}
	content, err := api.FetchMarkdownCF(creds.AccountID, creds.APIToken, url)
	if err != nil {
		return "", fmt.Errorf("cloudflare browser rendering: %w", err)
	}
	return content, nil
}

func fetchJina(originalURL string) (string, error) {
	req, err := http.NewRequest("GET", "https://r.jina.ai/"+originalURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/markdown")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Jina Reader %d for %s", resp.StatusCode, originalURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
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

func isMarkdownContentType(ct string) bool {
	return strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/x-markdown")
}

func isPlainText(ct string) bool {
	return strings.Contains(ct, "text/plain")
}

func parseGitHubBlobURL(rawURL string) (string, bool) {
	trimmed := strings.TrimPrefix(rawURL, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	parts := strings.SplitN(trimmed, "/blob/", 2)
	if len(parts) != 2 {
		return "", false
	}
	repo := parts[0]
	rest := parts[1]
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		return "", false
	}
	if idx := strings.IndexByte(rest, '?'); idx >= 0 {
		rest = rest[:idx]
	}
	if idx := strings.IndexByte(rest, '#'); idx >= 0 {
		rest = rest[:idx]
	}
	return repo + "/" + rest, true
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
