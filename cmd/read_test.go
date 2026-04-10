package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn and returns what it wrote to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestReadOutput_SummaryUsesTarget(t *testing.T) {
	// Build content long enough to trigger structural summary (>2000 lines)
	var b strings.Builder
	b.WriteString("# Section One\n\n")
	for i := 0; i < 2100; i++ {
		b.WriteString("Line of content.\n")
	}
	content := b.String()

	cmd := &ReadCmd{URL: ""} // empty — URL came from -d body
	target := "https://from-data-body.com/docs"

	out := captureStdout(t, func() {
		cmd.output("/tmp/cache.md", target, content, true)
	})

	if !strings.Contains(out, "[ctx:summary]") {
		t.Fatal("expected structural summary for long document")
	}
	if !strings.Contains(out, target) {
		t.Errorf("summary should reference target URL %q, got:\n%s", target, out[:min(len(out), 200)])
	}
	if strings.Contains(out, "ctx read  -s") {
		t.Error("summary contains empty URL (double space), target was not passed through")
	}
}

func TestReadOutput_SummaryUsesExplicitURL(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Heading\n\n")
	for i := 0; i < 2100; i++ {
		b.WriteString("Content line.\n")
	}
	content := b.String()

	target := "https://explicit.com/page"
	cmd := &ReadCmd{URL: target}

	out := captureStdout(t, func() {
		cmd.output("/tmp/cache.md", target, content, true)
	})

	if !strings.Contains(out, "ctx read "+target+" -s") {
		t.Errorf("summary should contain navigation hint with URL, got:\n%s", out[:min(len(out), 200)])
	}
}

// ===== skillReferencesHint =====

func TestSkillReferencesHint_GitHubSkillWithRefs(t *testing.T) {
	target := "https://github.com/AvdLee/swift-testing-agent-skill/blob/main/swift-testing-expert/SKILL.md"
	content := "---\nname: swift-testing-expert\n---\n\n# Swift Testing\n\nSee references/fundamentals.md for details.\n"

	got := skillReferencesHint(target, content)
	if !strings.Contains(got, "[ctx:skill-references]") {
		t.Fatal("expected skill-references hint for GitHub SKILL.md with references/")
	}
	if !strings.Contains(got, "github://AvdLee/swift-testing-agent-skill@main/swift-testing-expert/references/<file>") {
		t.Errorf("hint should contain github:// base path, got:\n%s", got)
	}
}

func TestSkillReferencesHint_GitHubScheme(t *testing.T) {
	target := "github://owner/repo@v2/my-skill/SKILL.md"
	content := "See references/guide.md\n"

	got := skillReferencesHint(target, content)
	if !strings.Contains(got, "github://owner/repo@v2/my-skill/references/<file>") {
		t.Errorf("should handle github:// scheme, got:\n%s", got)
	}
}

func TestSkillReferencesHint_NotSkillMD(t *testing.T) {
	target := "https://github.com/owner/repo/blob/main/README.md"
	content := "Some content with references/ mentioned.\n"

	got := skillReferencesHint(target, content)
	if strings.Contains(got, "[ctx:skill-references]") {
		t.Error("should not trigger for non-SKILL.md files")
	}
}

func TestSkillReferencesHint_NoReferencesInContent(t *testing.T) {
	target := "https://github.com/owner/repo/blob/main/my-skill/SKILL.md"
	content := "---\nname: simple-skill\n---\n\nNo refs here.\n"

	got := skillReferencesHint(target, content)
	if strings.Contains(got, "[ctx:skill-references]") {
		t.Error("should not trigger when content has no references/ pattern")
	}
}

func TestSkillReferencesHint_NonGitHub(t *testing.T) {
	target := "https://example.com/skills/SKILL.md"
	content := "See references/foo.md\n"

	got := skillReferencesHint(target, content)
	if strings.Contains(got, "[ctx:skill-references]") {
		t.Error("should not trigger for non-GitHub URLs")
	}
}

func TestSkillReferencesHint_PreservesOriginalContent(t *testing.T) {
	target := "https://github.com/owner/repo/blob/main/skill/SKILL.md"
	content := "Original content.\nSee references/foo.md\n"

	got := skillReferencesHint(target, content)
	if !strings.HasPrefix(got, content) {
		t.Error("hint should be appended, not replace original content")
	}
}

func TestReadOutput_ShortDocNeverSummarizes(t *testing.T) {
	content := "# Hello\n\nShort content.\n"
	cmd := &ReadCmd{URL: ""}

	out := captureStdout(t, func() {
		cmd.output("/tmp/cache.md", "https://example.com", content, true)
	})

	if strings.Contains(out, "[ctx:summary]") {
		t.Error("short doc should not produce structural summary")
	}
	if out != content {
		t.Errorf("short doc should be printed as-is, got:\n%s", out)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestReadFetch_GitHubRepoRootUsesReadmeAPI(t *testing.T) {
	oldClient := httpClient
	t.Cleanup(func() { httpClient = oldClient })
	t.Setenv("GITHUB_TOKEN", "test-token")

	readme := "# Portless\n\nDirect README fetch.\n"
	body := `{"content":"` + base64.StdEncoding.EncodeToString([]byte(readme)) + `","encoding":"base64"}`

	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "api.github.com" {
				t.Fatalf("unexpected host: %s", req.URL.Host)
			}
			if req.URL.Path != "/repos/vercel-labs/portless/readme" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	content, source, err := (&ReadCmd{}).fetch("https://github.com/vercel-labs/portless", nil)
	if err != nil {
		t.Fatalf("fetch returned error: %v", err)
	}
	if source != "github" {
		t.Fatalf("source = %q, want github", source)
	}
	if content != readme {
		t.Fatalf("content = %q, want %q", content, readme)
	}
}

func TestReadFetch_GitHubTreeDirectoryUsesContentsAPI(t *testing.T) {
	oldClient := httpClient
	t.Cleanup(func() { httpClient = oldClient })
	t.Setenv("GITHUB_TOKEN", "test-token")

	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "api.github.com" {
				t.Fatalf("unexpected host: %s", req.URL.Host)
			}
			if req.URL.Path != "/repos/TanStack/store/contents/docs" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			if got := req.URL.Query().Get("ref"); got != "main" {
				t.Fatalf("ref = %q, want %q", got, "main")
			}
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`[` +
						`{"name":"guide","type":"dir"},` +
						`{"name":"README.md","type":"file"},` +
						`{"name":"api.md","type":"file"}` +
						`]`,
				)),
			}, nil
		}),
	}

	content, source, err := (&ReadCmd{}).fetch("https://github.com/TanStack/store/tree/main/docs", nil)
	if err != nil {
		t.Fatalf("fetch returned error: %v", err)
	}
	if source != "github" {
		t.Fatalf("source = %q, want github", source)
	}

	want := strings.Join([]string{
		"[ctx:github-dir] github://TanStack/store@main/docs",
		"guide/",
		"api.md",
		"README.md",
	}, "\n")
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestFetchGitHub_GitHubSchemeDirectoryRendersListing(t *testing.T) {
	oldClient := httpClient
	t.Cleanup(func() { httpClient = oldClient })
	t.Setenv("GITHUB_TOKEN", "test-token")

	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/repos/o/r/contents/docs" {
				t.Fatalf("unexpected path: %s", req.URL.Path)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`[` +
						`{"name":"subdir","type":"dir"},` +
						`{"name":"file.md","type":"file"},` +
						`{"name":"link","type":"symlink"}` +
						`]`,
				)),
			}, nil
		}),
	}

	content, err := fetchGitHub("o/r/docs", "")
	if err != nil {
		t.Fatalf("fetchGitHub returned error: %v", err)
	}

	want := strings.Join([]string{
		"[ctx:github-dir] github://o/r/docs",
		"subdir/",
		"file.md",
		"link@",
	}, "\n")
	if content != want {
		t.Fatalf("content = %q, want %q", content, want)
	}
}

func TestReadFetch_GitHubIssueUsesIssueAPIs(t *testing.T) {
	oldClient := httpClient
	t.Cleanup(func() { httpClient = oldClient })
	t.Setenv("GITHUB_TOKEN", "test-token")

	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/repos/vercel-labs/portless/issues/12":
				return &http.Response{
					StatusCode: 200,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"title":"Issue title","body":"Issue body"}`)),
				}, nil
			case "/repos/vercel-labs/portless/issues/12/comments":
				page := req.URL.Query().Get("page")
				if page == "" || page == "1" {
					return &http.Response{
						StatusCode: 200,
						Header:     make(http.Header),
						Body: io.NopCloser(strings.NewReader(
							`[{"body":"First comment","user":{"login":"dio"}},{"body":"Second comment","user":{"login":"ethan"}}]`,
						)),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`[]`)),
				}, nil
			default:
				t.Fatalf("unexpected API path: %s", req.URL.Path)
				return nil, nil
			}
		}),
	}

	doc, err := fetchGitHubIssueDocument(gitHubIssueTarget{
		Owner:  "vercel-labs",
		Repo:   "portless",
		Number: 12,
	})
	if err != nil {
		t.Fatalf("fetchGitHubIssueDocument returned error: %v", err)
	}
	if doc.Title != "Issue title" || doc.Body != "Issue body" {
		t.Fatalf("unexpected issue document: %#v", doc)
	}
	if len(doc.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(doc.Comments))
	}
	if doc.Comments[0].Author != "dio" || doc.Comments[1].Author != "ethan" {
		t.Fatalf("unexpected comments: %#v", doc.Comments)
	}
}

func TestRenderGitHubIssue_AutoTruncatesWithHint(t *testing.T) {
	doc := gitHubIssueDocument{
		Title: "Large issue",
		Body:  "Body",
		Comments: []gitHubIssueComment{
			{Author: "a", Body: strings.Repeat("line\n", 900)},
			{Author: "b", Body: strings.Repeat("line\n", 900)},
		},
	}

	out, err := renderGitHubIssue(doc, "github://owner/repo/issues/7", issueCommentSelector{})
	if err != nil {
		t.Fatalf("renderGitHubIssue returned error: %v", err)
	}
	if !strings.Contains(out, "## Comments 1-1") {
		t.Fatalf("expected first comment to be included, got:\n%s", out)
	}
	if strings.Contains(out, "@b:") {
		t.Fatalf("second comment should not fit budget, got:\n%s", out)
	}
	if !strings.Contains(out, "ctx read github://owner/repo/issues/7 --comments 2-2") {
		t.Fatalf("missing continuation hint, got:\n%s", out)
	}
}

func TestRenderGitHubIssue_ExplicitCommentRange(t *testing.T) {
	doc := gitHubIssueDocument{
		Title: "Issue",
		Body:  "Body",
		Comments: []gitHubIssueComment{
			{Author: "a", Body: "one"},
			{Author: "b", Body: "two"},
			{Author: "c", Body: "three"},
		},
	}

	out, err := renderGitHubIssue(doc, "github://owner/repo/issues/7", issueCommentSelector{
		Start: 2,
		End:   3,
		Label: "2-3",
	})
	if err != nil {
		t.Fatalf("renderGitHubIssue returned error: %v", err)
	}
	if strings.Contains(out, "@a:") {
		t.Fatalf("unexpected first comment in ranged output:\n%s", out)
	}
	if !strings.Contains(out, "## Comments 2-3") || !strings.Contains(out, "@b:") || !strings.Contains(out, "@c:") {
		t.Fatalf("expected selected comment range, got:\n%s", out)
	}
}

func TestReadFetch_HTTPFailureFallsBackToCloudflare(t *testing.T) {
	oldHTTP := readHTTPFetcher
	oldCF := readCloudflareFetcher
	t.Cleanup(func() {
		readHTTPFetcher = oldHTTP
		readCloudflareFetcher = oldCF
	})

	readHTTPFetcher = func(url string) (string, error) {
		return "", fmt.Errorf("403 Forbidden")
	}
	readCloudflareFetcher = func(targetURL string, dataBody []byte) (string, error) {
		if targetURL != "https://docs.example.com" {
			t.Fatalf("targetURL = %q, want docs URL", targetURL)
		}
		if dataBody != nil {
			t.Fatalf("expected nil dataBody, got %q", dataBody)
		}
		return "# Rendered Doc\n", nil
	}

	var (
		content string
		source  string
		err     error
	)
	stderr := captureStderr(t, func() {
		content, source, err = (&ReadCmd{}).fetch("https://docs.example.com", nil)
	})
	if err != nil {
		t.Fatalf("fetch returned error: %v", err)
	}
	if source != "cloudflare" {
		t.Fatalf("source = %q, want cloudflare", source)
	}
	if content != "# Rendered Doc\n" {
		t.Fatalf("content = %q, want rendered content", content)
	}
	if !strings.Contains(stderr, "HTTP fetch failed (403 Forbidden), rendering via Cloudflare") {
		t.Fatalf("stderr = %q, want fallback diagnostic", stderr)
	}
}
