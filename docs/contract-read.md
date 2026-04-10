# ctx read — Behavioral Contract

This document defines the behavioral contract for `ctx read`. It is the single source of truth for how the command should behave across all input types, fetch paths, output modes, error conditions, and caching states.

Design principle: **Agent Experience (AX) first**. An agent calling `ctx read` must be able to:
1. Trust that stdout contains only document content plus, in one specific case (truncation), a trailing navigation block.
2. Distinguish success from failure unambiguously via exit code.
3. Act on any hint without contradicting itself or entering a retry loop.
4. Get identical stdout for the same logical request, regardless of cache state.

---

## 1. Input Classification

`ctx read` accepts a single positional argument. The argument is classified into exactly one input type by the **first matching rule**:

| Priority | Test | Input Type | Fetch Path |
|---|---|---|---|
| 1 | Starts with `file://`, `/`, `./`, `../`, `~/` | **local-file** | Direct filesystem read |
| 2 | Starts with `github://` | **github-scheme** | GitHub Contents API |
| 3 | Host is `github.com` AND path contains `/blob/` | **github-blob** | GitHub Contents API (best-effort ref parsing) |
| 4 | Host is `github.com` AND path contains `/tree/` with a non-empty subpath | **github-tree** | GitHub Contents API (directory listing) |
| 5 | Host is `github.com` AND path is repo root or `tree/<ref>` root | **github-readme** | GitHub README API |
| 6 | Host is `github.com` AND path matches `/issues/<id>` | **github-issue** | GitHub Issues API + comments |
| 7 | Starts with `http://` or `https://`, `-d` body provided | **cf-direct** | Cloudflare Browser Rendering |
| 8 | Starts with `http://` or `https://` | **http-negotiate** | HTTP with content negotiation → auto CF fallback |
| 9 | None of the above | **error** | Reject with usage hint |

Rules are evaluated top-down. GitHub repo roots are resolved to the repository README instead of falling through to generic HTML rendering.

---

## 2. Fetch Paths

### 2.1 local-file

- Read file from filesystem directly.
- No cache. No network. No `looksIncomplete` check. **No summary** (even for long files).
- Local files always output full content in default mode. This is critical because cache file paths point to local files — if local files were summarized, the agent would enter an infinite summary loop.
- `--toc` and `-s` still work on local files (heading-based navigation).
- Error on missing file: `file not found: <path>`

### 2.2 github-scheme / github-blob / github-tree / github-readme

**Ref handling:**

- `github://owner/repo@ref/path` — ref is the string between `@` and the next `/`. This is unambiguous because `@` is not valid in GitHub repository names.
- `https://github.com/owner/repo/blob/<ref>/path` — ref is parsed as the **first path segment** after `/blob/`.
- `https://github.com/owner/repo/tree/<ref>/path` — ref is parsed as the **first path segment** after `/tree/`, and the remaining path is treated as a directory target.
- `https://github.com/owner/repo` — resolves to the repository README on the default branch.
- `https://github.com/owner/repo/tree/<ref>` — resolves to the repository README for that ref.

**Slash-ref detection and rejection:**

Branch names can contain `/` (e.g. `feature/auth`), which makes `blob/<ref>/path` ambiguous — there is no way to distinguish `blob/feature/auth/src/main.go` (ref=`feature/auth`, path=`src/main.go`) from `blob/feature/auth/src/main.go` (ref=`feature`, path=`auth/src/main.go`) without querying the GitHub API.

On GitHub API 404, the error is returned as-is (the file may genuinely not exist). Additionally, a **stderr note** about possible ref ambiguity is emitted — the agent sees the real 404 error on stderr/exit and can decide whether the secondary hint is relevant:

- **Exit**: 1 (the original 404 error)
- **stderr note** (additive, not replacing the error): `Note: if the branch name contains '/', ref "X" may have been parsed incorrectly. Try: ctx read github://owner/repo@<ref>/path`

For `tree/<ref>` URLs, the same slash-ref ambiguity applies, but the retry target becomes `ctx read github://owner/repo@<ref>/README.md`.

This avoids turning a clear "not found" into a misleading "ref ambiguity" diagnosis. The note is secondary context, not a rewritten error.

The `github://` scheme supports refs containing `/` via URL-encoding: `github://owner/repo@feature%2Fauth/path`. The `@` marker unambiguously delimits the ref start; `%2F` within the ref prevents confusion with path separators. The ref is URL-decoded before being passed to the API.

For simple refs (no `/`): both URL formats work identically.

**API calls:**
- `GET /repos/{owner}/{repo}/contents/{path}?ref={ref}`
- `GET /repos/{owner}/{repo}/readme?ref={ref}` for repo-root README resolution
- Auth: `GITHUB_TOKEN` env → `GH_TOKEN` env → `gh auth token` CLI fallback → anonymous.
- **No `looksIncomplete` check.** GitHub returns authoritative content; short files are valid.

**Directory rendering:**
- If the GitHub Contents API returns an array, the target is a directory, not a file.
- stdout becomes an `ls`-style listing headed by `[ctx:github-dir] github://owner/repo@ref/path`.
- Directory entries end with `/`, symlinks end with `@`, submodules render as `name (submodule)`.
- Entries are sorted directories first, then files, then other types, each group case-insensitively by name.

### 2.3 github-issue

- Accepted inputs:
  - `https://github.com/owner/repo/issues/123`
  - `github://owner/repo/issues/123`
- Refs are not supported for issue targets. `github://owner/repo@main/issues/123` is rejected.
- API calls:
  - `GET /repos/{owner}/{repo}/issues/{id}`
  - `GET /repos/{owner}/{repo}/issues/{id}/comments?per_page=100&page=N`
- Default mode is **budgeted expansion**:
  - Always include issue title and body.
  - Append as many comments as fit within the issue render budget.
  - If not all comments fit, append a continuation hint: `ctx read github://owner/repo/issues/123 --comments X-Y`
- `--comments 1-3` reads a specific inclusive range.
- `--comments all` forces all comments to be rendered.
- Issue rendering never uses the structural-summary mode. The issue-specific continuation hint replaces it.

### 2.4 cf-direct (with `-d` body)

- Build request body via merge pipeline (settings → site headers → `-d` body → overrides).
- Call CF Browser Rendering `/markdown` endpoint.
- **No `looksIncomplete` check.** CF already performed full JS rendering. If the result is still sparse, the issue is the page itself (paywall, anti-bot, empty page).

### 2.5 http-negotiate

Two-phase fetch:

**Phase 1: HTTP content negotiation**
- Send `Accept: text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, application/json;q=0.7, application/xml;q=0.6, text/html;q=0.5`
- Accept response directly (no CF fallback) if Content-Type matches any of:
  - `text/markdown`, `text/x-markdown` — markdown
  - `text/plain` — plain text
  - `application/json`, `text/json` — JSON (excellent context source)
  - `application/xml`, `text/xml` — XML
  - `application/yaml`, `text/yaml`, `application/x-yaml` — YAML
  - Any `text/*` subtype not listed below
- Fall through to Phase 2 if Content-Type is:
  - `text/html`, `application/xhtml+xml` — needs browser rendering
  - Any other `application/*` type not in the accept list above (binary, unknown)
- **Source = `http`** for all Phase 1 accepted responses.
- On HTTP error (non-2xx): return error immediately. Do NOT fall through to Phase 2.

**Phase 2: Cloudflare fallback** (triggered by HTML response OR `looksIncomplete` content)
- Log `Content looks incomplete, rendering via Cloudflare...` to **stderr** only when Phase 1 returned text that looks incomplete.
- Build request body via merge pipeline.
- Call CF Browser Rendering `/markdown` endpoint. **Source = `cloudflare`**.
- On CF credential error: return actionable error.

**`looksIncomplete` auto-retries with CF.** If Phase 1 returns text content that looks incomplete (< 500 chars, or contains JS-required signals like "enable javascript", "loading..."), Phase 2 kicks in automatically. No manual `-f` flag needed.

---

## 3. Output Protocol

### 3.1 stdout

stdout carries the document content. In default mode, a long document is replaced by a **structural summary** (see 3.3) that forces the agent to use `-s` for precise reading. Agents should treat stdout as the document itself.

Three output modes, selected by flags:

| Flag | Mode | stdout content |
|---|---|---|
| (none) | **default** | Full content (short docs), or structural summary (long docs) |
| `--toc` | **toc** | Heading outline (one heading per line, with line counts) |
| `-s <expr>` | **section** | Extracted section(s), full content, no truncation |

### 3.2 stderr

stderr carries transient diagnostics that are NOT part of the result:
- Fetch progress: `Content looks incomplete, rendering via Cloudflare...`
- Quality warnings: `Content may be incomplete...`
- Empty-content warnings: `No content returned...`

An agent sees stdout on success. A human watching the terminal also sees stderr.

### 3.3 Long document handling (default mode only)

**Problem with naive line truncation**: Showing the first N lines creates position bias (agent only sees early content) and encourages lazy reading (agent stops after "enough" content, hallucinating about the rest). The agent must understand the **complete structure** before reading any section.

**Threshold**: 2000 lines.

**Short documents (≤ threshold)**: Output full content as-is. No modification.

**Long documents (> threshold)**: Replace content with a **structural summary** — a compressed view of the entire document that preserves all headings with line counts and gives a proportional preview of each section's content:

```
[ctx:summary] 5000 lines, 12 sections. Read sections: ctx read <url> -s <number>
Full content: ~/.cache/ctx/abc123.md

# 1 Getting Started (45 lines)
This library provides a unified interface for browser rendering.
It supports markdown extraction, screenshots, and structured data.
...

## 1.1 Installation (12 lines)
npm install ctx-render
...

## 1.2 Configuration (89 lines)
Create a `ctx.config.js` file in your project root. The following
options are available for customizing the rendering pipeline:
...

# 2 API Reference (3200 lines)
Complete reference for all public methods and types.
...

## 2.1 render(url, options) (450 lines)
Renders a URL and returns the result in the specified format.
Accepts an options object with the following properties:
...

## 2.2 screenshot(url, options) (380 lines)
Captures a screenshot of the rendered page. Returns a Buffer
containing the PNG image data. Supports full-page capture
and element-specific screenshots via CSS selectors.
...
```

**Construction rules**:
1. Parse headings via `ParseHeadings` (same as `--toc` and `-s`).
2. For each section, compute `sectionLines` (lines between this heading and the next).
3. Extract the first N **non-empty** lines of body text (after the heading line), where N = `clamp(sectionLines / 10, 2, 5)` — proportional to section size, minimum 2 lines, maximum 5 lines.
4. Append `...` to the last preview line to signal continuation.
5. If a section has no body text (heading only), show just the heading with `(0 lines)`.
6. Headings use the numbered format: `# 1 Title (N lines)`, `## 1.1 Subtitle (N lines)` — same numbering system as `--toc` and `-s`.

**Preview sizing rationale**: A 300-line section gets 5 lines of preview (max). A 15-line section gets 2 lines (min). This ensures large sections get enough context for the agent to judge relevance, while small sections don't waste space.

**Why this works for AX**:
- Agent sees the **complete document structure**, not a position-biased prefix.
- Each section has enough preview to judge relevance (topic sentences + size).
- Line counts let the agent estimate information density per section.
- Agent is forced to use `-s <number>` to read specific sections — intentional, precise reading.
- No content is privileged by position. Section 2.2 gets the same treatment as Section 1.1.

**The `[ctx:summary]` marker**: First line of output. Machine-readable tag that unambiguously signals this is a summary, not the full document. Contains total line count, section count, and the `-s` usage hint. No additional footer warning is needed — the marker is sufficient.

**Headingless long documents** (JSON, XML, YAML, plain text logs, etc.):

When `ParseHeadings` finds zero headings, the structural summary cannot use section-based navigation (`-s` is meaningless without headings). Instead, fall back to a **line-window summary**:

```
[ctx:summary] 5000 lines, no sections. Full content: ~/.cache/ctx/abc123.md

Lines 1-5:
{"openapi": "3.1.0", "info": {"title": "Payment API", "version": "2.1.0"},
 "paths": {"/payments": {"get": {"summary": "List payments",
...

Lines 1000-1004:
    "/refunds/{id}": {"get": {"summary": "Get refund status",
...

Lines 2500-2504:
    "/webhooks": {"post": {"summary": "Register webhook endpoint",
...

Lines 4996-5000:
    }}}
...
```

Construction rules for headingless documents:
1. Sample up to 5 evenly-spaced windows across the document, each showing 3-5 lines.
2. First window always starts at line 1. Last window always includes the final lines.
3. Between windows, show `...` to indicate skipped content.
4. The `[ctx:summary]` line says `no sections` and omits the `-s` hint. Instead, it provides only the cache file path.

Agent action for headingless documents: read the cache file directly (e.g. via `ctx read <cache_path>` or the agent's own file-read tool). Since there are no sections to navigate, the full file is the only option.

### 3.4 Cache consistency guarantee

**For the same logical request, stdout MUST be byte-identical whether served from cache or from a fresh fetch.**

This is enforced by:
1. Only clean content (no hints) is stored in cache.
2. All quality hints (`looksIncomplete`, empty-content) go to stderr, not stdout.
3. Structural summary is generated at output time from cached content (same threshold, same algorithm). It is not stored separately.

---

## 4. Hint Rules

### 4.1 Core principle: auto-recover, don't hint

The old approach was to hint "re-run with `-f`" — this wasted a round-trip. Now:
- **`looksIncomplete` content**: auto-retries with CF rendering (no hint needed).
- **HTML response**: auto-falls back to CF rendering (no hint needed).
- **Empty content after CF**: hint with possible causes (stderr). No actionable retry exists.

### 4.2 When NOT to hint

- **local-file**: never. The file is what it is.
- **github-scheme / github-blob / github-tree / github-readme / github-issue**: never. GitHub API returns authoritative content.
- **After auto-retry**: never for content quality. CF already did full rendering.

### 4.3 Hint table (all go to stderr)

| Condition | Applies to source | Hint (stderr) |
|---|---|---|
| Content is empty string | any | `No content returned for {url}. Possible causes: authentication required (ctx site set {domain} ...), anti-bot protection, or the page is genuinely empty.` |
| Content is empty string | `github` | (no hint — GitHub 404 is an error, not empty content; empty file is valid) |

### 4.4 Hint content rules

1. **Hints go to stderr only.** They never appear on stdout.
2. **Empty content is not a diagnosis.** List possible causes instead of assuming authentication.

---

## 5. Cache Identity

Cache key = `SHA256("markdown" + NUL + canonical_url)`

Canonical URL rules:
- `https://github.com/owner/repo/blob/ref/path` → `github://owner/repo@ref/path`
- `https://github.com/owner/repo/tree/ref/path` → `github://owner/repo@ref/path`
- `github://owner/repo@ref/path` → as-is
- `github://owner/repo/path` (no ref) → as-is (different key from any `@ref` variant)
- All other URLs → as-is (no normalization)

Cache entry = `{key}.md` (content) + `{key}.meta.json` (metadata incl. `source` field).

TTL = `settings.jsonc → cache.ttl`, fallback 1 hour.

`--no-cache` bypasses lookup but still stores the fresh result.

---

## 6. Error Protocol

All errors are returned as non-zero exit code + message to stderr. stdout is empty on error.

Error messages must be:
1. **Contextual**: include the URL or path that failed.
2. **Actionable**: include what the user/agent should do next.
3. **No stack traces**: wrap errors with human-readable context.

| Condition | Error message |
|---|---|
| Local file not found | `file not found: {path}` |
| GitHub API 404 | `GitHub API 404 for {owner}/{repo}/{path}: not found. Check the repository and path.` |
| GitHub API 403 (rate limit) | `GitHub API rate limited. Set GITHUB_TOKEN or run: gh auth login` |
| HTTP non-2xx | `HTTP {status} for {url}` |
| CF not configured | `cloudflare not configured — run: ctx auth login cloudflare` |
| CF API error | `cloudflare rendering failed for {url}: {error}` |
| Invalid section expr | `invalid section expression "{expr}": {detail} — use --toc to see available sections` |
| No sections matched | `no sections matched "{expr}" — use: ctx read {url} --toc` |

---

## 7. Flag Interactions

| Flag combo | Behavior |
|---|---|
| `--toc` | Output TOC only. No truncation. No hints. |
| `-s 1.2` | Output section(s) only. No truncation. No hints. |
| `--toc -s 1.2` | `--toc` wins (checked first). |
| `--no-cache` | Bypass cache lookup, but still store result. |
| `--no-cache --toc` | Fresh fetch, then output TOC. |

---

## 8. Data Flow Diagram

```
Input
  │
  ├─ local-file ──────────────────────────────────────────────┐
  │                                                           │
  ├─ github-scheme/blob ─── fetchGitHub(path, ref) ──────────┤
  │                                                           │
  ├─ cf-direct (-d) ─── BuildRequestBody → CF /markdown ─────┤
  │                                                           │
  └─ http-negotiate ─── fetchHTTP ──┬─ complete text ─────────┤
                                    │                         │
                                    └─ html/incomplete ── CF ──┤
                                                              │
                                              ┌───────────────┘
                                              │
                                              ▼
                                    (content, source)
                                              │
                              ┌───────────────┼────────────────┐
                              │               │                │
                         source=http    source=github    source=cloudflare
                              │               │                │
                     looksIncomplete?     (no check)      (no check)
                       empty check?                      empty check?
                              │               │                │
                       stderr hint       (no hint)       stderr hint
                              │               │           (if empty)
                              │               │                │
                              └───────┬───────┘────────────────┘
                                      │
                                      ▼
                              cache.Store(content)
                                      │
                                      ▼
                               output(content)
                                      │
                         ┌────────────┼────────────┐
                         │            │            │
                       --toc       -s expr     (default)
                         │            │            │
                        TOC        sections    truncate?
                                                   │
                                            ┌──────┴──────┐
                                            │             │
                                         ≤2000        >2000 lines
                                          lines          │
                                            │       structural
                                         full        summary
                                        content    [ctx:summary]
```

---

## 9. Behavioral Test Cases

These are the acceptance criteria. Each case specifies input, expected stdout, expected stderr, and exit code.

### 9.1 Local file

```
INPUT:  ctx read ./README.md  (file exists, 50 lines)
STDOUT: <file content>
STDERR: (empty)
EXIT:   0
```

```
INPUT:  ctx read ./missing.md  (file does not exist)
STDOUT: (empty)
STDERR: file not found: /abs/path/to/missing.md
EXIT:   1
```

```
INPUT:  ctx read ./huge-local-file.md  (3000 lines, local file)
STDOUT: <full 3000 lines, NO summary>
STDERR: (empty)
EXIT:   0
NOTE:   local files NEVER get summarized. This is critical because cache paths
        are local files — summarizing them would create an infinite loop.
```

### 9.2 GitHub

```
INPUT:  ctx read https://github.com/owner/repo/blob/v2.0/docs/guide.md
STDOUT: <file content from GitHub API with ?ref=v2.0>
STDERR: (empty)
EXIT:   0
CACHE:  key derived from "github://owner/repo@v2.0/docs/guide.md"
```

```
INPUT:  ctx read github://owner/repo@main/README.md  (file is 100 chars)
STDOUT: <short content, no incomplete hint anywhere>
STDERR: (empty)
EXIT:   0
NOTE:   looksIncomplete is NOT applied to github source
```

```
INPUT:  ctx read https://github.com/owner/repo/blob/feature/auth/src/main.go
        (parsed as ref="feature", path="auth/src/main.go" → GitHub API returns 404)
STDOUT: (empty)
STDERR: GitHub API 404: {"message":"Not Found"...}
        Note: if the branch name contains '/', ref "feature" may have been parsed incorrectly.
        Try: ctx read github://owner/repo@<ref>/auth/src/main.go
EXIT:   1
NOTE:   Original 404 error preserved. Ref ambiguity hint is additive, not a replacement.
        Agent sees the real error first; the note is secondary context.
```

```
INPUT:  ctx read github://owner/repo@feature%2Fauth/src/main.go
STDOUT: <correct file content, ref decoded to "feature/auth">
STDERR: (empty)
EXIT:   0
NOTE:   %2F in ref is URL-decoded before API call: ?ref=feature/auth
```

### 9.3 SSR page (server returns markdown)

```
INPUT:  ctx read https://docs.example.com/guide.md  (returns text/markdown)
STDOUT: <markdown content>
STDERR: (empty)
EXIT:   0
```

```
INPUT:  ctx read https://api.example.com/v1/config  (returns application/json)
STDOUT: <JSON content>
STDERR: (empty)
EXIT:   0
NOTE:   JSON accepted directly in Phase 1, no CF fallback
```

```
INPUT:  ctx read https://spa.example.com/docs  (returns text/html)
STDOUT: <CF-rendered markdown>
STDERR: (empty)
EXIT:   0
NOTE:   looksIncomplete is NOT checked (source=cloudflare)
```

### 9.4 Incomplete HTTP content → auto CF retry

```
INPUT:  ctx read https://example.com/page  (returns text/plain, 200 chars with "loading..." signal)
STDOUT: <CF-rendered content>
STDERR: Content looks incomplete, rendering via Cloudflare...
EXIT:   0
NOTE:   looksIncomplete triggered auto-retry via CF. No manual -f needed.
```

### 9.6 Long document → structural summary

```
INPUT:  ctx read https://huge-docs.example.com/api  (5000 lines, 12 sections)
STDOUT: [ctx:summary] 5000 lines, 12 sections. Read sections: ctx read https://huge-docs.example.com/api -s <number>
        Full content: ~/.cache/ctx/{hash}.md

        # 1 Getting Started (45 lines)
        This library provides a unified interface for browser rendering.
        It supports markdown extraction, screenshots, and structured data.
        ...

        ## 1.1 Installation (12 lines)
        npm install ctx-render
        ...

        ... (all 12 sections listed with proportional previews)

STDERR: (empty)
EXIT:   0
NOTE:   agent MUST use -s to read specific sections.
```

```
INPUT:  ctx read https://small-docs.example.com/guide  (500 lines)
STDOUT: <full content, no summary, no modification>
STDERR: (empty)
EXIT:   0
NOTE:   below threshold (2000), output as-is
```

```
INPUT:  ctx read https://api.example.com/openapi.json  (5000 lines, application/json, no headings)
STDOUT: [ctx:summary] 5000 lines, no sections. Full content: ~/.cache/ctx/{hash}.md

        Lines 1-5:
        {"openapi": "3.1.0", "info": {"title": "Payment API",
        ...

        Lines 1000-1004:
            "/refunds/{id}": {"get": {"summary": "Get refund status",
        ...

        Lines 4996-5000:
            }}}
        ...

STDERR: (empty)
EXIT:   0
NOTE:   no headings → line-window summary. Agent reads cache file directly.
```

### 9.7 Cache consistency

```
# First call (cache miss, source=http, content > 500 chars, no JS signals)
INPUT:  ctx read https://example.com/doc
STDOUT: <content>
EXIT:   0

# Second call (cache hit)
INPUT:  ctx read https://example.com/doc
STDOUT: <byte-identical content>
EXIT:   0
```

### 9.8 Section extraction

```
INPUT:  ctx read https://example.com/doc -s 99  (section does not exist)
STDOUT: (empty)
STDERR: no sections matched "99" — use: ctx read https://example.com/doc --toc
EXIT:   1
```

### 9.9 CF not configured (auto-fallback path)

```
INPUT:  ctx read https://spa.example.com  (HTTP returns HTML, CF not configured)
STDOUT: (empty)
STDERR: cloudflare not configured — run: ctx auth login cloudflare
EXIT:   1
```

### 9.10 Empty content

```
INPUT:  ctx read https://protected.example.com  (CF returns empty string after auto-fallback)
STDOUT: (empty)
STDERR: No content returned for https://protected.example.com.
        Possible causes: authentication required (ctx site set protected.example.com ...),
        anti-bot protection, or the page is genuinely empty.
EXIT:   0
```
