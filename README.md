# ctx

Full-document search and reading for AI agents. Find any library's docs, read the complete source — not RAG fragments.

```bash
ctx docs react "useEffect cleanup"     # find doc sources via Context7 index
ctx read <url>                          # read full document as clean markdown
ctx crawl <docs-site> --limit 20       # pull an entire docs section at once
```

## The Problem

AI coding agents need documentation, but current tools make trade-offs:

- **RAG-based tools** (ctx7, etc.) return 60-200 token fragments — too small for real understanding
- **Full-doc tools** (ref, etc.) return complete pages but search accuracy is inconsistent

**ctx** combines the best of both: ctx7's search index to *find* the right documents, then fetches the full originals via GitHub API, HTTP content negotiation, or headless browser rendering.

## Install

```bash
go install github.com/ethan-huo/ctx@latest
```

<details>
<summary>Build from source</summary>

```bash
make build    # compile to bin/ctx
make install  # build + symlink to ~/.local/bin/ctx
```

Requires Go 1.25+.
</details>

## Workflow

### 1. Search → Read

```bash
ctx docs mlx-swift "GPU stream thread safety"   # find relevant doc URLs
ctx read <url>                                    # read as clean markdown
```

Long documents (>2000 lines) automatically produce a structural summary with numbered sections:

```bash
ctx read <url>             # returns summary with section numbers
ctx read <url> -s 2.1      # read specific section
ctx read <url> -s "1-3,5"  # combine sections
ctx read <url> --toc       # compact heading outline
```

### 2. Browser Superpowers

When plain HTTP isn't enough, ctx uses [Cloudflare Browser Rendering](https://developers.cloudflare.com/browser-rendering/) for full JS-rendered pages:

| Need | Command |
|---|---|
| Read a JS-rendered SPA | `ctx read <url>` |
| Extract specific DOM elements | `ctx scrape <url> -s "table.api-params"` |
| Pull multiple pages from a docs site | `ctx crawl <url> --limit 50 --depth 2` |
| Screenshot a page | `ctx screenshot <url> --full-page` |
| Explore a site's link structure | `ctx links <url> --internal-only` |
| Extract structured data with AI | `ctx json <url> --prompt "Extract pricing tiers"` |

All commands support `-d` for full API control (cookies, viewport, JS injection, etc.):

```jsonc
{
  url: "https://example.com",
  cookies: "session=abc",
  viewport: {width: 1920, height: 1080},
  addScriptTag: [{content: "document.querySelector('.nav')?.remove()"}]
}
```

### 3. Per-Domain Auth

Store headers that auto-inject into all requests for a domain:

```bash
ctx site set example.com Cookie "session=abc"
ctx site set example.com Authorization "Bearer token123"
ctx site ls
```

## How `read` Resolves URLs

| URL Pattern | Strategy |
|---|---|
| `/path`, `./path`, `file://` | Direct file read |
| `github://owner/repo@ref/path` | GitHub Contents API |
| `https://github.com/.../blob/...` | Auto-converted to GitHub API |
| `https://github.com/.../tree/<ref>/<path>` | GitHub directory listing via Contents API |
| `https://github.com/owner/repo` | Auto-resolved to repository README |
| `https://github.com/owner/repo/issues/123` | Auto-resolved to issue title/body/comments |
| Any `https://` (text/markdown/JSON/XML) | Direct fetch |
| Any `https://` (HTML/SPA) | Cloudflare Browser Rendering fallback |

Issue reads auto-expand comments until a line budget is reached, then append a continuation hint like `ctx read github://owner/repo/issues/123 --comments 9-20`. Use `--comments 1-3` or `--comments all` to override.
GitHub directory reads return an `ls`-style listing headed by the canonical `github://...` path, so agents can immediately drill into a child path instead of trying to parse GitHub HTML.

## Authentication

```bash
ctx auth login ctx7          # Context7 (OAuth PKCE, opens browser)
ctx auth login cloudflare    # Cloudflare Browser Rendering
ctx auth status              # check what's configured
```

GitHub reads use your `gh auth` token automatically.

## Agent Integration

ctx ships with a [skill definition](skills/ctx/SKILL.md) for AI agents (Claude Code, Cursor, etc.) that teaches them the full search → read → navigate → scrape workflow. Install it with your agent's skill mechanism.

## Environment Variables

| Variable | Purpose |
|---|---|
| `GITHUB_TOKEN` / `GH_TOKEN` | GitHub API token (fallback: `gh auth token`) |
| `CONTEXT7_BASE_URL` | Override Context7 API base URL |
| `CONTEXT7_API_KEY` | Context7 API key (alternative to OAuth) |
