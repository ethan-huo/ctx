# ctx

Library documentation finder — uses [Context7](https://context7.com) index to locate documentation sources, then reads full documents instead of RAG chunks.

## Why

Tools like ctx7 and ref both solve "find docs for AI agents" but with trade-offs:

- **ctx7** has great search but returns fragmented RAG chunks (60-200 tokens each)
- **ref** returns full documents but search accuracy is inconsistent

ctx takes ctx7's search index and discards the chunks, keeping only the source URLs. Then it reads the full original documents via GitHub API or HTTP with markdown content negotiation.

## Install

```bash
go install github.com/ethan-huo/ctx@latest
```

Or build from source:

```bash
make build    # compile to bin/ctx
make install  # build + symlink to ~/.local/bin/ctx
```

Requires Go 1.25+.

## Usage

```bash
# Find a library by name
ctx search react-native

# Find documentation sources for a library
ctx docs mlx-swift "GPU stream thread safety"
ctx docs sparkle "appcast auto update"

# Read a full document
ctx read github://ml-explore/mlx-swift/Source/MLX/Documentation.docc/MLXArray.md
ctx read https://sparkle-project.org/documentation/index

# Table of contents and section extraction
ctx read --toc https://example.com/docs
ctx read -s 1.2 https://example.com/docs

# Browser rendering commands
ctx screenshot https://example.com --full-page
ctx links https://example.com --internal-only
ctx scrape https://example.com -s ".article" --text-only
ctx json https://example.com --prompt "Extract all product names and prices"
ctx crawl https://docs.example.com --limit 50 --depth 2

# Per-domain header management
ctx site set example.com Cookie "session=abc"
ctx site ls
```

## Authentication

ctx shares credentials with ctx7 (`~/.config/ctx/credentials.yaml`).

```bash
# Login to Context7 (opens browser, OAuth PKCE)
ctx auth login ctx7

# Configure Cloudflare Browser Rendering
ctx auth login cloudflare

# Check status
ctx auth status

# Logout (clears all credentials)
ctx auth logout
```

GitHub reads use your `gh auth` token automatically.

## How `read` works

| URL | Strategy |
|---|---|
| Local path / `file://` | Direct file read |
| `github://owner/repo[@ref]/path` | GitHub Contents API |
| `https://github.com/.../blob/...` | Auto-converted to GitHub API |
| Any `https://` | `Accept: text/markdown` negotiation → Cloudflare Browser Rendering fallback |

Flags:

| Flag | Short | Description |
|------|-------|-------------|
| `--full` | `-f` | Force Cloudflare Browser Rendering (skip HTTP negotiation) |
| `--toc` | | Show heading outline with section numbers and line counts |
| `--section` | `-s` | Extract section(s) by number (e.g., `1`, `1-3`, `1.2,3.1-5.1`) |
| `--no-cache` | | Bypass cache lookup (still stores result) |
| `--data` | `-d` | Cloudflare API request body (JSON5, `@file`, or `-` for stdin). Implies `-f`. |

For documents over 2000 lines, `read` automatically produces a structural summary with numbered headings and line counts. Use `-s <number>` to read specific sections.

## Browser Rendering Commands

All commands below use [Cloudflare Browser Rendering](https://developers.cloudflare.com/browser-rendering/) and support `-d` for passing full API request bodies as JSON5.

```bash
ctx screenshot <url> [--full-page] [--selector <css>] [-o file]
ctx links <url> [--internal-only] [--visible-only]
ctx scrape <url> -s <selector> [--text-only]
ctx json <url> --prompt <text> [--schema @file]
ctx crawl <url> [--limit N] [--depth N] [--include/--exclude patterns]
```

Use `-d @file.json5` or `-d -` (stdin/heredoc) to pass cookies, auth, viewport, and other CF API parameters.

Common `-d` parameters:

```jsonc
{
  url: "https://example.com",
  cookies: "session=abc",
  viewport: {width: 1920, height: 1080},
  waitForSelector: ".content-loaded",
  addScriptTag: [{content: "document.querySelector('.nav')?.remove()"}]
}
```

## Per-Domain Headers (`site`)

Manage custom headers that are auto-injected into all Cloudflare requests for matching domains.

```bash
ctx site ls [domain]                    # list domains or headers
ctx site set <domain> <key> <value>     # set a single header
ctx site set <domain> @headers.json5    # bulk import from file
ctx site del <domain> [key]             # delete domain or header
```

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `GITHUB_TOKEN` / `GH_TOKEN` | GitHub API token (fallback: `gh auth token`) |
| `CONTEXT7_BASE_URL` | Override Context7 API base URL |
| `CONTEXT7_API_KEY` | Context7 API key (alternative to OAuth) |

## AI Agent Integration

The `skills/ctx/` directory contains a SKILL.md for Claude Code / Cursor / similar tools. Install it with your agent's skill mechanism.
