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
	"sort"
	"strconv"
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
	URL      string `arg:"" help:"URL or local path to read (github://, https://, file://, or path)" optional:""`
	NoCache  bool   `help:"Bypass cache, always fetch fresh"`
	TOC      bool   `help:"Show heading outline with section numbers"`
	Section  string `short:"s" help:"Section(s) to extract (e.g. 1, 1-3, 1.2,3.1-5.1,6.2)"`
	Comments string `help:"Issue comment range to include (e.g. 1-3, all)"`
}

const summaryThreshold = 2000
const issueCommentAutoLineBudget = 1500

type gitHubIssueTarget struct {
	Owner     string
	Repo      string
	Number    int
	Canonical string
}

type gitHubIssueDocument struct {
	Title    string
	Body     string
	Comments []gitHubIssueComment
}

type gitHubIssueComment struct {
	Author string
	Body   string
}

type issueCommentSelector struct {
	All   bool
	Start int
	End   int
	Label string
}

var readHTTPFetcher = fetchHTTP
var readCloudflareFetcher = fetchCloudflare

func (c *ReadCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	url := c.URL

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

	issueTarget, isIssueTarget, err := parseGitHubIssueTarget(target)
	if err != nil {
		return err
	}
	if c.Comments != "" && !isIssueTarget {
		return fmt.Errorf("--comments is only supported for GitHub issue targets")
	}
	if isIssueTarget {
		return c.runGitHubIssue(target, issueTarget)
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
			content := skillReferencesHint(target, string(data))
			return c.output(cache.Path(cacheKey, ".md"), target, content, true)
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

	// Empty content hint (stderr)
	if strings.TrimSpace(content) == "" {
		fmt.Fprintf(os.Stderr, "No content returned for %s. Possible causes: authentication required (ctx site set %s ...), anti-bot protection, or the page is genuinely empty.\n", target, extractDomainFromURL(target))
	}

	content = skillReferencesHint(target, content)
	return c.output(cache.Path(cacheKey, ".md"), target, content, true)
}

func (c *ReadCmd) runGitHubIssue(target string, issueTarget gitHubIssueTarget) error {
	if c.URL == "" {
		return fmt.Errorf("GitHub issue targets are not supported with -d body input")
	}

	selector, err := parseIssueCommentSelector(c.Comments)
	if err != nil {
		return err
	}

	cacheMode := "auto"
	if selector.All {
		cacheMode = "all"
	} else if selector.Label != "" {
		cacheMode = selector.Label
	}
	cacheKey := cache.Key("markdown", issueTarget.Canonical, "comments="+cacheMode)

	if !c.NoCache {
		if data, _, ok := cache.Lookup(cacheKey, ".md"); ok {
			return c.output(cache.Path(cacheKey, ".md"), issueTarget.Canonical, string(data), false)
		}
	}

	doc, err := fetchGitHubIssueDocument(issueTarget)
	if err != nil {
		return err
	}

	content, err := renderGitHubIssue(doc, issueTarget.Canonical, selector)
	if err != nil {
		return err
	}

	_ = cache.Store(cacheKey, []byte(content), ".md", cache.Meta{
		URL:    issueTarget.Canonical,
		Source: "github-issue",
	})

	if strings.TrimSpace(content) == "" {
		fmt.Fprintf(os.Stderr, "No content returned for %s.\n", issueTarget.Canonical)
	}

	return c.output(cache.Path(cacheKey, ".md"), issueTarget.Canonical, content, false)
}

// skillReferencesHint appends a reference discovery hint when reading a GitHub-hosted SKILL.md.
// No API call needed — the SKILL.md body already lists its references.
// We just provide the base path so the agent knows how to ctx read them.
func skillReferencesHint(target, content string) string {
	canonical := canonicalizeURL(target)
	if !strings.HasPrefix(canonical, "github://") {
		return content
	}
	if !strings.HasSuffix(canonical, "/SKILL.md") {
		return content
	}
	if !strings.Contains(content, "references/") {
		return content
	}
	base := strings.TrimSuffix(canonical, "SKILL.md") + "references/"
	return content + "\n---\n[ctx:skill-references] Read references with: ctx read " + base + "<file>\n"
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
		if path, ref, ok := parseGitHubTreeURL(url); ok {
			content, err := fetchGitHub(path, ref)
			if err != nil && strings.Contains(err.Error(), "GitHub API 404") {
				// tree/<ref>/path has the same slash-ref ambiguity as blob/<ref>/path.
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
		if owner, repo, ref, ok := parseGitHubRepoReadmeURL(url); ok {
			content, err := fetchGitHubReadme(owner, repo, ref)
			if err != nil && ref != "" && strings.Contains(err.Error(), "GitHub API 404") {
				// tree/<ref> has the same slash-ref ambiguity as blob/<ref>/path.
				fmt.Fprintf(os.Stderr,
					"Note: if the branch name contains '/', ref %q may have been parsed incorrectly.\n"+
						"Try: ctx read github://%s/%s@<ref>/README.md\n",
					ref, owner, repo)
			}
			return content, "github", err
		}
	}

	// -d provided: direct Cloudflare rendering with custom body
	if dataBody != nil {
		content, err := fetchCloudflare(url, dataBody)
		return content, "cloudflare", err
	}

	// Default: HTTP with markdown negotiation, CF fallback for any failure.
	content, httpErr := readHTTPFetcher(url)

	// Good text content that looks complete — use it.
	if httpErr == nil && content != "" && !looksIncomplete(content) {
		return content, "http", nil
	}

	// HTTP failed (anti-bot 403/503, timeout, etc.) or returned
	// HTML/incomplete content — fallback to CF browser rendering.
	if httpErr != nil {
		fmt.Fprintf(os.Stderr, "HTTP fetch failed (%v), rendering via Cloudflare...\n", httpErr)
	} else if content != "" {
		fmt.Fprintf(os.Stderr, "Content looks incomplete, rendering via Cloudflare...\n")
	}
	content, err := readCloudflareFetcher(url, nil)
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

func parseIssueCommentSelector(raw string) (issueCommentSelector, error) {
	if strings.TrimSpace(raw) == "" {
		return issueCommentSelector{}, nil
	}
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "all") {
		return issueCommentSelector{All: true, Label: "all"}, nil
	}
	if strings.Contains(trimmed, ",") {
		return issueCommentSelector{}, fmt.Errorf("invalid comment range %q (expected 3, 1-3, or all)", raw)
	}
	if strings.Contains(trimmed, "-") {
		parts := strings.SplitN(trimmed, "-", 2)
		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || start < 1 {
			return issueCommentSelector{}, fmt.Errorf("invalid comment range %q (expected positive numbers)", raw)
		}
		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || end < start {
			return issueCommentSelector{}, fmt.Errorf("invalid comment range %q (expected start-end with end >= start)", raw)
		}
		return issueCommentSelector{Start: start, End: end, Label: fmt.Sprintf("%d-%d", start, end)}, nil
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 1 {
		return issueCommentSelector{}, fmt.Errorf("invalid comment range %q (expected 3, 1-3, or all)", raw)
	}
	return issueCommentSelector{Start: n, End: n, Label: trimmed}, nil
}

func parseGitHubIssueTarget(raw string) (gitHubIssueTarget, bool, error) {
	if strings.HasPrefix(raw, "github://") {
		path, ref := parseGitHubScheme(strings.TrimPrefix(raw, "github://"))
		parts := strings.Split(path, "/")
		if len(parts) != 4 || parts[2] != "issues" {
			return gitHubIssueTarget{}, false, nil
		}
		if ref != "" {
			return gitHubIssueTarget{}, false, fmt.Errorf("GitHub issue targets do not support refs; use github://owner/repo/issues/<id>")
		}
		number, err := strconv.Atoi(parts[3])
		if err != nil || number < 1 {
			return gitHubIssueTarget{}, false, fmt.Errorf("invalid GitHub issue target %q", raw)
		}
		return gitHubIssueTarget{
			Owner:     parts[0],
			Repo:      parts[1],
			Number:    number,
			Canonical: fmt.Sprintf("github://%s/%s/issues/%d", parts[0], parts[1], number),
		}, true, nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return gitHubIssueTarget{}, false, nil
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(u.Path), "/"), "/")
	if len(parts) != 4 || parts[2] != "issues" {
		return gitHubIssueTarget{}, false, nil
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number < 1 {
		return gitHubIssueTarget{}, false, fmt.Errorf("invalid GitHub issue target %q", raw)
	}
	return gitHubIssueTarget{
		Owner:     parts[0],
		Repo:      parts[1],
		Number:    number,
		Canonical: fmt.Sprintf("github://%s/%s/issues/%d", parts[0], parts[1], number),
	}, true, nil
}

func fetchGitHubIssueDocument(target gitHubIssueTarget) (gitHubIssueDocument, error) {
	var issue struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	issueURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", target.Owner, target.Repo, target.Number)
	if err := fetchGitHubJSON(issueURL, &issue); err != nil {
		return gitHubIssueDocument{}, err
	}

	var comments []gitHubIssueComment
	for page := 1; ; page++ {
		apiURL := fmt.Sprintf(
			"https://api.github.com/repos/%s/%s/issues/%d/comments?per_page=100&page=%d",
			target.Owner, target.Repo, target.Number, page,
		)
		var pageComments []struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := fetchGitHubJSON(apiURL, &pageComments); err != nil {
			return gitHubIssueDocument{}, err
		}
		for _, comment := range pageComments {
			comments = append(comments, gitHubIssueComment{
				Author: comment.User.Login,
				Body:   comment.Body,
			})
		}
		if len(pageComments) < 100 {
			break
		}
	}

	return gitHubIssueDocument{
		Title:    issue.Title,
		Body:     issue.Body,
		Comments: comments,
	}, nil
}

func renderGitHubIssue(doc gitHubIssueDocument, canonicalTarget string, selector issueCommentSelector) (string, error) {
	var b strings.Builder
	writeIssueHeader(&b, doc)

	if len(doc.Comments) == 0 {
		return b.String(), nil
	}

	start := 1
	end := len(doc.Comments)
	if !selector.All && selector.Label != "" {
		if selector.Start > len(doc.Comments) {
			return "", fmt.Errorf("comment range %s is out of bounds; issue has %d comments", selector.Label, len(doc.Comments))
		}
		start = selector.Start
		end = selector.End
		if end > len(doc.Comments) {
			end = len(doc.Comments)
		}
		writeIssueComments(&b, doc.Comments, start, end)
		return b.String(), nil
	}
	if selector.All {
		writeIssueComments(&b, doc.Comments, start, end)
		return b.String(), nil
	}

	if countLines(b.String()) >= issueCommentAutoLineBudget {
		writeIssueCommentHint(&b, canonicalTarget, 1, len(doc.Comments), len(doc.Comments))
		return b.String(), nil
	}

	displayedEnd := 0
	var commentBlocks []string
	for i, comment := range doc.Comments {
		block := formatIssueComment(i+1, comment)
		next := strings.Join(commentBlocks, "") + block
		candidate := b.String() + "\n---\n## Comments 1-" + strconv.Itoa(i+1) + "\n\n" + next
		if countLines(candidate) > issueCommentAutoLineBudget {
			break
		}
		commentBlocks = append(commentBlocks, block)
		displayedEnd = i + 1
	}

	if displayedEnd > 0 {
		b.WriteString("\n---\n")
		fmt.Fprintf(&b, "## Comments 1-%d\n\n", displayedEnd)
		for _, block := range commentBlocks {
			b.WriteString(block)
		}
	}
	if displayedEnd < len(doc.Comments) {
		writeIssueCommentHint(&b, canonicalTarget, displayedEnd+1, len(doc.Comments), displayedEnd)
	}

	return b.String(), nil
}

func writeIssueHeader(b *strings.Builder, doc gitHubIssueDocument) {
	fmt.Fprintf(b, "# %s\n\n", strings.TrimSpace(doc.Title))
	body := strings.TrimSpace(doc.Body)
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
}

func writeIssueComments(b *strings.Builder, comments []gitHubIssueComment, start, end int) {
	b.WriteString("\n---\n")
	if start == end {
		fmt.Fprintf(b, "## Comment %d\n\n", start)
	} else {
		fmt.Fprintf(b, "## Comments %d-%d\n\n", start, end)
	}
	for i := start - 1; i < end; i++ {
		b.WriteString(formatIssueComment(i+1, comments[i]))
	}
}

func formatIssueComment(index int, comment gitHubIssueComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "@%s:\n", comment.Author)
	body := strings.TrimSpace(comment.Body)
	if body != "" {
		b.WriteString(body)
	} else {
		b.WriteString("(empty comment)")
	}
	b.WriteString("\n\n---\n\n")
	return b.String()
}

func writeIssueCommentHint(b *strings.Builder, canonicalTarget string, nextStart, total, shown int) {
	if nextStart > total {
		return
	}
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	fmt.Fprintf(b,
		"[ctx:comments] 已显示 %d/%d 条评论。继续读取：\nctx read %s --comments %d-%d\n",
		shown, total, canonicalTarget, nextStart, total,
	)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// canonicalizeURL normalizes GitHub URLs to github:// form for cache key consistency.
// When a ref is present, the format is github://owner/repo@ref/path.
func canonicalizeURL(url string) string {
	if strings.Contains(url, "github.com") {
		if path, ref, ok := parseGitHubBlobURL(url); ok {
			return formatGitHubScheme(path, ref)
		}
		if path, ref, ok := parseGitHubTreeURL(url); ok {
			return formatGitHubScheme(path, ref)
		}
		if issueTarget, ok, err := parseGitHubIssueTarget(url); err == nil && ok {
			return issueTarget.Canonical
		}
		if owner, repo, ref, ok := parseGitHubRepoReadmeURL(url); ok {
			return formatGitHubScheme(owner+"/"+repo+"/README.md", ref)
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
	return fetchGitHubContent(apiURL, path, ref)
}

func fetchGitHubReadme(owner, repo, ref string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", owner, repo)
	if ref != "" {
		apiURL += "?ref=" + url.QueryEscape(ref)
	}
	return fetchGitHubContent(apiURL, owner+"/"+repo+"/README.md", ref)
}

func fetchGitHubContent(apiURL, path, ref string) (string, error) {
	resp, err := doGitHubAPIRequest(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var entries []gitHubDirectoryEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return "", err
		}
		return renderGitHubDirectory(path, ref, entries), nil
	}

	var result gitHubFileContent
	if err := json.Unmarshal(body, &result); err != nil {
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

type gitHubFileContent struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type gitHubDirectoryEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

func renderGitHubDirectory(path, ref string, entries []gitHubDirectoryEntry) string {
	sort.Slice(entries, func(i, j int) bool {
		left := directoryEntrySortKey(entries[i])
		right := directoryEntrySortKey(entries[j])
		if left != right {
			return left < right
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	var b strings.Builder
	b.WriteString("[ctx:github-dir] ")
	b.WriteString(formatGitHubScheme(path, ref))
	b.WriteByte('\n')
	for _, entry := range entries {
		b.WriteString(formatGitHubDirectoryEntry(entry))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func directoryEntrySortKey(entry gitHubDirectoryEntry) int {
	switch entry.Type {
	case "dir":
		return 0
	case "file":
		return 1
	default:
		return 2
	}
}

func formatGitHubDirectoryEntry(entry gitHubDirectoryEntry) string {
	switch entry.Type {
	case "dir":
		return entry.Name + "/"
	case "symlink":
		return entry.Name + "@"
	case "submodule":
		return entry.Name + " (submodule)"
	default:
		return entry.Name
	}
}

func fetchGitHubJSON(apiURL string, dest any) error {
	resp, err := doGitHubAPIRequest(apiURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(dest)
}

func doGitHubAPIRequest(apiURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if token := ghToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 403 && strings.Contains(string(body), "rate limit") {
			return nil, fmt.Errorf("GitHub API rate limited. Set GITHUB_TOKEN or run: gh auth login")
		}
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(body))
	}
	return resp, nil
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

func parseGitHubTreeURL(rawURL string) (path string, ref string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "github.com" {
		return "", "", false
	}

	parts := strings.Split(strings.Trim(strings.TrimSpace(u.Path), "/"), "/")
	if len(parts) < 5 || parts[2] != "tree" {
		return "", "", false
	}
	if parts[0] == "" || parts[1] == "" || parts[3] == "" {
		return "", "", false
	}

	subpath := strings.Join(parts[4:], "/")
	if subpath == "" {
		return "", "", false
	}

	return parts[0] + "/" + parts[1] + "/" + subpath, parts[3], true
}

func parseGitHubRepoReadmeURL(rawURL string) (owner string, repo string, ref string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", false
	}
	if u.Host != "github.com" {
		return "", "", "", false
	}

	trimmed := strings.Trim(strings.TrimSpace(u.Path), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return "", "", "", false
	}
	owner, repo = parts[0], parts[1]
	if owner == "" || repo == "" {
		return "", "", "", false
	}

	// Repo root: https://github.com/owner/repo
	if len(parts) == 2 {
		return owner, repo, "", true
	}

	// Explicit root ref: https://github.com/owner/repo/tree/<ref>
	if len(parts) == 4 && parts[2] == "tree" {
		return owner, repo, parts[3], true
	}

	return "", "", "", false
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
