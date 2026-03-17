---
name: ctx
description: >-
  Search documentation for libraries, frameworks, SDKs, and APIs by name and query.
  Read any URL or local file as clean markdown (GitHub, doc sites, JS-rendered SPAs).
  Navigate large documents with TOC outline and section extraction.
  Screenshot, extract links, scrape elements, and crawl websites via Cloudflare Browser Rendering.
---

# ctx — Library Documentation Finder

Find library documentation, then read the full source documents. Two-step workflow: **search → read**.

Binary: `ctx`

## Workflow

### Step 1: Find documentation sources

```bash
ctx docs <library-name> "<query>"
```

This returns a list of relevant documents with descriptions, plus their URLs.

```bash
# Examples
ctx docs mlx-swift "GPU stream thread safety"
ctx docs sparkle "appcast auto update configuration"
ctx docs convex "Swift client authentication"
```

If you already know the library ID (format `/owner/repo`), pass it directly:

```bash
ctx docs /ml-explore/mlx-swift "lazy evaluation"
```

### Step 2: Read the full documents

Pick the most relevant URL(s) from Step 1 and read them:

```bash
ctx read <url>
```

The `read` command auto-detects the URL type:

| URL pattern                       | Strategy                                                                 |
| --------------------------------- | ------------------------------------------------------------------------ |
| Local path / `file://`            | Direct file read (no cache)                                              |
| `github://owner/repo/path`        | GitHub API (authenticated via `gh auth`)                                 |
| `https://github.com/.../blob/...` | GitHub API (auto-converted)                                              |
| `https://...` (serves markdown)   | Direct fetch with `Accept: text/markdown`                                |
| `https://...` (serves HTML)       | Cloudflare Browser Rendering → clean markdown                            |
| `https://...` (JS/SPA page)       | `ctx read -f <url>` → Cloudflare Browser Rendering (skip HTTP attempt) |

Remote results are cached for 1 hour at `~/.cache/ctx/`. Use `--no-cache` to force a fresh fetch.

### Navigating large documents

Documents over 2000 lines are automatically truncated to the first 1000 lines, with a hint appended at the end of stdout. Use `--toc` and `-s` to navigate:

```bash
# View the document outline (section numbers + line counts)
ctx read <url> --toc
# output:
#   1 Getting Started (68)
#   1.1 Installation (12)
#   1.2 Quick Start (25)
#   2 API Reference (300)
#   2.1 Authentication (45)

# Read a specific section by number
ctx read <url> -s 1.2

# Read multiple sections
ctx read <url> -s "1,3.1,6.2"

# Read a range of sections (by TOC position, inclusive)
ctx read <url> -s "1-3"

# Mix ranges and singles
ctx read <url> -s "1-2,3.2-5.1,6.2"
```

Use `--toc` first to find section numbers and estimate size, then `-s` to read specific sections.

### Putting it together

```bash
# 1. Find
ctx docs react "useEffect cleanup async"
# output:
#   1. **React Hooks Reference**
#      - ...
#   ---
#   - github://facebook/react/docs/hooks-reference.md

# 2. Read the most relevant one
ctx read github://facebook/react/docs/hooks-reference.md
```

## Writing Good Queries

The query directly affects result quality. Be specific.

| Quality | Example                                                          |
| ------- | ---------------------------------------------------------------- |
| Good    | `"SwiftUI NavigationStack path binding programmatic navigation"` |
| Good    | `"Express.js middleware error handling async"`                   |
| Bad     | `"navigation"`                                                   |
| Bad     | `"middleware"`                                                   |

Include the programming language or framework name when ambiguous.

## When to use `search` instead of `docs`

Use `ctx search` when you need to **find the right library first**, before querying its docs:

```bash
# "Which library is this?"
ctx search swift-testing
ctx search convex "mobile client"
```

This returns a ranked list of matching libraries with IDs you can feed to `docs`.

## Browser Rendering Commands

All browser rendering commands use Cloudflare Browser Rendering. Requires `ctx auth login cloudflare` first.

Every command supports `-d` / `--data` for passing the **full CF API request body** as JSON5:
- Inline: `-d '{url: "https://example.com", cookies: [{name: "sid", value: "abc"}]}'`
- File reference: `-d @/tmp/request.json`
- Stdin/heredoc: `-d -`

When `-d` is combined with flags, flags override the corresponding fields in the body. Use `-d @session.json` to reuse auth/cookies across multiple requests.

Full CF API parameter reference: https://developers.cloudflare.com/browser-rendering/rest-api/

### Screenshot — capture a webpage as an image

```bash
ctx screenshot <url>
ctx screenshot <url> --full-page
ctx screenshot <url> --selector ".main-content" -o output.png

# Full API control via -d
ctx screenshot -d '{url: "https://example.com", viewport: {width: 390, height: 844}, screenshotOptions: {type: "jpeg", quality: 80}}'
```

Output: prints the file path to stdout. The image file can be read by multimodal AI agents.

**When to use**: page has visual information (UI, charts, layouts) that markdown can't capture.

### Links — extract all links from a page

```bash
ctx links <url>
ctx links <url> --internal-only
ctx links <url> --visible-only
```

Output: one URL per line.

**When to use**: explore a documentation site's structure before selectively reading pages.

### Scrape — extract specific elements by CSS selector

```bash
ctx scrape <url> -s "h1" -s "p.description"
ctx scrape <url> -s "table.api-params" --text-only
```

Output: JSON (or plain text with `--text-only`).

**When to use**: extract specific parts of a page (API tables, code blocks) without full-page markdown.

### Crawl — bulk-crawl a website for documentation

```bash
ctx crawl <url> --limit 20
ctx crawl <url> --limit 50 --include "*/api/*" --exclude "*/changelog/*"

# Async mode
ctx crawl <url> --no-wait       # prints job ID
ctx crawl <job-id>              # check status / get results
ctx crawl <job-id> --cancel     # cancel job
```

Output: markdown content per page, separated by `---`.

**When to use**: pull an entire documentation site for comprehensive context.

### JSON — AI-powered structured data extraction

```bash
ctx json <url> --prompt "Extract all API endpoints with their HTTP methods and parameters"
ctx json <url> --prompt "List all pricing tiers" --schema @schema.json
```

Output: JSON. The AI model is configured globally in `~/.config/ctx/credentials.yaml` under `ai:` and auto-injected.

**When to use**: extract structured data from a page when you need specific fields, not just raw text.

### Site — manage per-domain headers

```bash
ctx site ls                                          # list domains
ctx site ls example.com                              # list headers
ctx site set example.com Authorization "Bearer xxx"  # set header
ctx site set example.com Cookie @/tmp/cookie.txt     # value from file
ctx site set example.com @headers.json5              # bulk import
ctx site del example.com Authorization               # delete header
ctx site del example.com                             # delete domain
```

Site headers are auto-injected into all CF requests matching the domain. Use this for persistent auth/cookies.

## Configuration

See `references/settings.md` for full configuration reference.
