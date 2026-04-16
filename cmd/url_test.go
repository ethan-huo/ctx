package cmd

import (
	"strings"
	"testing"
)

// ===== parseGitHubBlobURL =====

func TestParseGitHubBlobURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPath string
		wantRef  string
		wantOK   bool
	}{
		{
			name:     "standard blob URL with branch",
			input:    "https://github.com/ml-explore/mlx-swift/blob/main/docs/guide.md",
			wantPath: "ml-explore/mlx-swift/docs/guide.md",
			wantRef:  "main",
			wantOK:   true,
		},
		{
			name:     "blob URL with tag ref",
			input:    "https://github.com/owner/repo/blob/v2.0.1/README.md",
			wantPath: "owner/repo/README.md",
			wantRef:  "v2.0.1",
			wantOK:   true,
		},
		{
			name:     "blob URL with commit SHA",
			input:    "https://github.com/owner/repo/blob/abc123def/src/main.go",
			wantPath: "owner/repo/src/main.go",
			wantRef:  "abc123def",
			wantOK:   true,
		},
		{
			name:     "deep nested path",
			input:    "https://github.com/a/b/blob/dev/x/y/z/file.txt",
			wantPath: "a/b/x/y/z/file.txt",
			wantRef:  "dev",
			wantOK:   true,
		},
		{
			name:     "strips query params",
			input:    "https://github.com/o/r/blob/main/file.go?plain=1",
			wantPath: "o/r/file.go",
			wantRef:  "main",
			wantOK:   true,
		},
		{
			name:     "strips fragment",
			input:    "https://github.com/o/r/blob/main/file.go#L42",
			wantPath: "o/r/file.go",
			wantRef:  "main",
			wantOK:   true,
		},
		{
			name:   "not a blob URL (tree)",
			input:  "https://github.com/owner/repo/tree/main/src",
			wantOK: false,
		},
		{
			name:   "not a blob URL (plain repo)",
			input:  "https://github.com/owner/repo",
			wantOK: false,
		},
		{
			name:   "blob with no path after ref",
			input:  "https://github.com/owner/repo/blob/main",
			wantOK: false,
		},
		{
			name:   "non-github URL",
			input:  "https://example.com/some/path",
			wantOK: false,
		},
		{
			name:     "http (not https)",
			input:    "http://github.com/o/r/blob/v1/file.md",
			wantPath: "o/r/file.md",
			wantRef:  "v1",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ref, ok := parseGitHubBlobURL(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
		})
	}
}

func TestParseGitHubTreeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPath string
		wantRef  string
		wantOK   bool
	}{
		{
			name:     "directory under explicit ref",
			input:    "https://github.com/TanStack/store/tree/main/docs",
			wantPath: "TanStack/store/docs",
			wantRef:  "main",
			wantOK:   true,
		},
		{
			name:     "deep directory strips fragment and query",
			input:    "https://github.com/o/r/tree/release/docs/api?plain=1#top",
			wantPath: "o/r/docs/api",
			wantRef:  "release",
			wantOK:   true,
		},
		{
			name:   "tree root stays with readme parser",
			input:  "https://github.com/o/r/tree/main",
			wantOK: false,
		},
		{
			name:   "blob url is not tree",
			input:  "https://github.com/o/r/blob/main/file.go",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ref, ok := parseGitHubTreeURL(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
		})
	}
}

func TestParseGitHubRepoReadmeURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantRef   string
		wantOK    bool
	}{
		{
			name:      "plain repo root",
			input:     "https://github.com/vercel-labs/portless",
			wantOwner: "vercel-labs",
			wantRepo:  "portless",
			wantRef:   "",
			wantOK:    true,
		},
		{
			name:      "repo root with trailing slash and fragment",
			input:     "https://github.com/vercel-labs/portless/#readme",
			wantOwner: "vercel-labs",
			wantRepo:  "portless",
			wantRef:   "",
			wantOK:    true,
		},
		{
			name:      "tree root with explicit ref",
			input:     "https://github.com/vercel-labs/portless/tree/main",
			wantOwner: "vercel-labs",
			wantRepo:  "portless",
			wantRef:   "main",
			wantOK:    true,
		},
		{
			name:   "repo subpage is not readme target",
			input:  "https://github.com/vercel-labs/portless/issues",
			wantOK: false,
		},
		{
			name:   "blob URL is handled elsewhere",
			input:  "https://github.com/vercel-labs/portless/blob/main/README.md",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ref, ok := parseGitHubRepoReadmeURL(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
		})
	}
}

func TestParseGitHubIssueTarget(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantCanonical string
		wantNumber    int
		wantOK        bool
		wantErr       string
	}{
		{
			name:          "github issue url",
			input:         "https://github.com/vercel-labs/portless/issues/12",
			wantCanonical: "github://vercel-labs/portless/issues/12",
			wantNumber:    12,
			wantOK:        true,
		},
		{
			name:          "github issue scheme",
			input:         "github://vercel-labs/portless/issues/12",
			wantCanonical: "github://vercel-labs/portless/issues/12",
			wantNumber:    12,
			wantOK:        true,
		},
		{
			name:    "refs are rejected for issue targets",
			input:   "github://vercel-labs/portless@main/issues/12",
			wantErr: "do not support refs",
		},
		{
			name:   "non issue github url",
			input:  "https://github.com/vercel-labs/portless/pull/12",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok, err := parseGitHubIssueTarget(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if target.Canonical != tt.wantCanonical {
				t.Fatalf("canonical = %q, want %q", target.Canonical, tt.wantCanonical)
			}
			if target.Number != tt.wantNumber {
				t.Fatalf("number = %d, want %d", target.Number, tt.wantNumber)
			}
		})
	}
}

// ===== formatGitHubScheme + parseGitHubScheme round-trip =====

func TestGitHubSchemeRoundTrip(t *testing.T) {
	tests := []struct {
		path string
		ref  string
		want string
	}{
		{"owner/repo/file.md", "main", "github://owner/repo@main/file.md"},
		{"owner/repo/file.md", "v2.0", "github://owner/repo@v2.0/file.md"},
		{"owner/repo/deep/path/file.go", "abc123", "github://owner/repo@abc123/deep/path/file.go"},
		{"owner/repo/file.md", "", "github://owner/repo/file.md"},
	}

	for _, tt := range tests {
		uri := formatGitHubScheme(tt.path, tt.ref)
		if uri != tt.want {
			t.Errorf("formatGitHubScheme(%q, %q) = %q, want %q", tt.path, tt.ref, uri, tt.want)
		}

		// Round-trip: parse back
		gotPath, gotRef := parseGitHubScheme(uri[len("github://"):])
		if gotPath != tt.path {
			t.Errorf("round-trip path = %q, want %q", gotPath, tt.path)
		}
		if gotRef != tt.ref {
			t.Errorf("round-trip ref = %q, want %q", gotRef, tt.ref)
		}
	}
}

// ===== canonicalizeURL =====

func TestCanonicalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// GitHub blob → github:// with ref
		{
			"https://github.com/o/r/blob/main/file.md",
			"github://o/r@main/file.md",
		},
		{
			"https://github.com/o/r/blob/v2.0/src/lib.go",
			"github://o/r@v2.0/src/lib.go",
		},
		// Repo roots resolve to the repository README.
		{
			"https://github.com/owner/repo",
			"github://owner/repo/README.md",
		},
		{
			"https://github.com/owner/repo/tree/main",
			"github://owner/repo@main/README.md",
		},
		{
			"https://github.com/owner/repo/tree/main/docs",
			"github://owner/repo@main/docs",
		},
		{
			"https://github.com/owner/repo/issues/12",
			"github://owner/repo/issues/12",
		},
		{
			"https://www.youtube.com/watch?v=rIwgZWzUKm8&t=15s",
			"https://www.youtube.com/watch?v=rIwgZWzUKm8",
		},
		// Non-read github URL stays as-is
		{
			"https://github.com/owner/repo/issues",
			"https://github.com/owner/repo/issues",
		},
		// Non-github URL stays as-is
		{
			"https://docs.example.com/guide",
			"https://docs.example.com/guide",
		},
	}

	for _, tt := range tests {
		got := canonicalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("canonicalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Different refs produce different canonical URLs (cache correctness)
func TestCanonicalizeURL_DifferentRefsAreDifferent(t *testing.T) {
	a := canonicalizeURL("https://github.com/o/r/blob/main/file.md")
	b := canonicalizeURL("https://github.com/o/r/blob/v2.0/file.md")
	if a == b {
		t.Errorf("different refs produced same canonical URL: %q", a)
	}
}

// ===== localPath =====

func TestLocalPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{"file:// scheme", "file:///tmp/test.md", true},
		{"absolute path", "/tmp/test.md", true},
		{"relative dot", "./test.md", true},
		{"relative dotdot", "../test.md", true},
		{"home tilde", "~/test.md", true},
		{"https URL", "https://example.com", false},
		{"github scheme", "github://o/r/file", false},
		{"bare filename", "test.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := localPath(tt.input)
			if ok != tt.wantOK {
				t.Errorf("localPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
		})
	}
}

// ===== looksIncomplete =====

func TestLooksIncomplete(t *testing.T) {
	short := "Short content."
	if looksIncomplete(short) {
		t.Error("short content without JS signals should not look incomplete")
	}

	if !looksIncomplete("   ") {
		t.Error("empty content should look incomplete")
	}

	jsRequired := make([]byte, 600)
	for i := range jsRequired {
		jsRequired[i] = 'x'
	}
	copy(jsRequired, []byte("This page requires JavaScript"))
	if !looksIncomplete(string(jsRequired)) {
		t.Error("JS-required content should look incomplete")
	}

	good := make([]byte, 600)
	for i := range good {
		good[i] = 'a'
	}
	if looksIncomplete(string(good)) {
		t.Error("long content without JS signals should not look incomplete")
	}
}

// ===== normalizeURL (docs.go) =====

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"https://github.com/o/r/blob/main/file.md",
			"github://o/r@main/file.md",
		},
		{
			"https://github.com/owner/repo",
			"github://owner/repo/README.md",
		},
		{
			"https://github.com/owner/repo/tree/main/docs",
			"github://owner/repo@main/docs",
		},
		{
			"https://github.com/owner/repo/issues/12",
			"github://owner/repo/issues/12",
		},
		{
			"https://youtu.be/rIwgZWzUKm8?si=test",
			"https://www.youtube.com/watch?v=rIwgZWzUKm8",
		},
		{
			"https://docs.example.com/guide",
			"https://docs.example.com/guide",
		},
	}

	for _, tt := range tests {
		got := normalizeURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ===== parseGitHubScheme with %2F (URL-encoded slash ref) =====

func TestParseGitHubScheme_URLEncodedRef(t *testing.T) {
	path, ref := parseGitHubScheme("owner/repo@feature%2Fauth/src/main.go")
	if ref != "feature/auth" {
		t.Errorf("ref = %q, want %q", ref, "feature/auth")
	}
	if path != "owner/repo/src/main.go" {
		t.Errorf("path = %q, want %q", path, "owner/repo/src/main.go")
	}
}

func TestParseGitHubScheme_PlainRef(t *testing.T) {
	path, ref := parseGitHubScheme("owner/repo@v2.0/src/main.go")
	if ref != "v2.0" {
		t.Errorf("ref = %q, want %q", ref, "v2.0")
	}
	if path != "owner/repo/src/main.go" {
		t.Errorf("path = %q, want %q", path, "owner/repo/src/main.go")
	}
}

// ===== isDirectlyReadable =====

func TestIsDirectlyReadable(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"text/markdown", true},
		{"text/markdown; charset=utf-8", true},
		{"text/plain", true},
		{"application/json", true},
		{"application/json; charset=utf-8", true},
		{"application/xml", true},
		{"text/xml", true},
		{"application/yaml", true},
		{"text/csv", true},   // generic text/* accepted
		{"text/html", false}, // needs browser rendering
		{"text/html; charset=utf-8", false},
		{"application/octet-stream", false},
		{"image/png", false},
	}

	for _, tt := range tests {
		got := isDirectlyReadable(tt.ct)
		if got != tt.want {
			t.Errorf("isDirectlyReadable(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

// ===== extractDomainFromURL =====

func TestExtractDomainFromURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/page", "example.com"},
		{"https://sub.example.com:8080/page", "sub.example.com"},
		{"http://localhost:3000", "localhost"},
	}

	for _, tt := range tests {
		got := extractDomainFromURL(tt.input)
		if got != tt.want {
			t.Errorf("extractDomainFromURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ===== isRealURL =====

func TestIsRealURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://docs.example.com/guide", true},
		{"http://example.com", true},
		{"ftp://example.com", false},
		{"not-a-url", false},
		{"https://context7.com/something", false},
		{"https://example.com/context7_cache", false},
	}

	for _, tt := range tests {
		got := isRealURL(tt.input)
		if got != tt.want {
			t.Errorf("isRealURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
