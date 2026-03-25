---
name: ctx
description: >-
  The `ctx` command, Search and read documentation for libraries, frameworks, SDKs, and APIs by name and query.
  Read any URL or local file as clean markdown (GitHub repos, doc sites, JS-rendered SPAs).
  Navigate large documents with TOC and section extraction.
  Screenshot webpages, extract links, scrape elements by CSS selector, extract structured
  JSON data, and crawl entire documentation sites.
---

## Core Workflow: search → read

### 1. Find documentation sources

```bash
ctx docs <library-name> "<query>"
```

Returns relevant document URLs. Be specific with queries — include language/framework name when ambiguous.

```bash
ctx docs mlx-swift "GPU stream thread safety"
ctx docs react "useEffect cleanup async"
ctx docs /ml-explore/mlx-swift "lazy evaluation"   # direct library ID
```

Use `ctx search <name> [query]` first if you need to find the right library.

### 2. Read documents

```bash
ctx read <url>
```

| Flag         | Short | Default  | Description                                                               |
| ------------ | ----- | -------- | ------------------------------------------------------------------------- |
| (positional) |       | optional | URL or local path (`https://`, `github://`, `file://`, `/path`, `./path`) |
| `--full`     | `-f`  | false    | Force full JS rendering (skip HTTP attempt)                               |
| `--no-cache` |       | false    | Bypass cache, always fetch fresh                                          |
| `--toc`      |       | false    | Show heading outline with section numbers and line counts                 |
| `--section`  | `-s`  |          | Section(s) to extract (e.g. `1`, `1-3`, `1.2,3.1`)                        |
| `--data`     | `-d`  |          | CF API request body (JSON5, `@file`, or stdin). Implies `-f`.             |

Auto-detects URL type and fetches accordingly:

- Local file / `file://` → direct read (always full content, no summary)
- `github://owner/repo@ref/path` → GitHub API (supports `@ref` for versioned docs)
- `https://github.com/.../blob/...` → GitHub API (auto-converted)
- `https://...` (markdown/text/JSON/XML/YAML) → direct fetch
- `https://...` (HTML/SPA) → full JS rendering fallback; use `-f` to skip the HTTP attempt

When rendering via Cloudflare, a content-density heuristic automatically strips navigation, sidebar, header, and footer noise. This works for most sites without any configuration.

If the default cleanup selects the wrong content block on a specific site, use `-d` with `addScriptTag` to **replace** it with your own extraction logic:

```bash
ctx read -d '{url: "https://example.com", addScriptTag: [{content: "document.body.innerHTML = document.querySelector(\".doc-body\").outerHTML"}]}'
```

To find the right selector, probe the page first:

1. `ctx scrape <url> -s "main" -s "article" -s ".content"` — try common selectors, see which returns the content you need
2. Use the matched selector in `addScriptTag` to override the default cleanup

Other useful `-d` parameters: `cookies`, `waitForSelector`, `gotoOptions.waitUntil`, `viewport`.

**stdout/stderr contract**: stdout is always clean document content. Diagnostic hints (incomplete content, empty page warnings) go to stderr only.

**IMPORTANT**: Never pipe ctx output through `head`, `tail`, `cut`, or any truncation. ctx already manages content length — large documents return a structural summary automatically. Truncating destroys the summary structure and section references.

### 3. Navigate large documents

Documents over 2000 lines produce a **structural summary** instead of the full content. The output starts with `[ctx:summary]` and shows every section heading with line counts and a preview:

```
[ctx:summary] 5000 lines, 12 sections. Read sections: ctx read <url> -s <number>
Full content: ~/.cache/ctx/{hash}.md

# 1 Getting Started (45 lines)
This library provides a unified interface...
...

## 1.1 Installation (12 lines)
npm install ctx-render
...
```

**How to work with summaries**:

1. Read the summary to understand the full document structure
2. Use `-s` to read the sections you need: `ctx read <url> -s 2.1`
3. Combine sections: `ctx read <url> -s "1-3,5.2"`
4. Or read the cache file path directly for the full raw content

Use `--toc` for a compact outline without previews.

## Choosing the right tool

Start with `ctx read`. Escalate when it's not enough:

| Situation                                | Tool                              | Example                                  |
| ---------------------------------------- | --------------------------------- | ---------------------------------------- |
| Need one page's full content             | `ctx read <url>`                  | Read a doc page                          |
| Page is too long (>2000 lines)           | `ctx read <url> -s <section>`     | Navigate via structural summary          |
| Only need specific elements from a page  | `ctx scrape <url> -s "selector"`  | Extract an API table, skip sidebar noise |
| Need content from many pages on one site | `ctx crawl <url> --limit N`       | Pull an entire docs section              |
| Don't know which pages to read           | `ctx links <url>` then `ctx read` | Explore site structure first             |
| Need visual info (UI, charts, layouts)   | `ctx screenshot <url>`            | Inspect rendered page; `--scroll 900` for below-the-fold |
| Need structured data extraction          | `ctx json <url> --prompt "..."`   | Extract pricing tiers as JSON            |

Common compositions:

- **Docs research**: `ctx docs <lib> "<query>"` → `ctx read <url>` → `ctx read <url> -s N` for deep sections
- **Full-site understanding**: `ctx crawl <url> --limit 20 --depth 2` (replaces manual links + read loop)
- **Surgical extraction**: `ctx read <url> --toc` to find target → `ctx scrape <url> -s "table.params"` to extract it

## Browser Rendering Commands

These commands require `ctx auth login cloudflare`. Each has a dedicated reference — read it before first use:

| Command                          | Use when                                        | Reference                |
| -------------------------------- | ----------------------------------------------- | ------------------------ |
| `ctx screenshot <url>`           | Need visual information (UI, charts, layouts)   | references/screenshot.md |
| `ctx links <url>`                | Explore a site's link structure before reading  | references/links.md      |
| `ctx scrape <url> -s "selector"` | Extract specific elements (tables, code blocks) | references/scrape.md     |
| `ctx json <url> --prompt "..."`  | Extract structured data as JSON                 | references/json.md       |
| `ctx crawl <url>`                | Pull multiple pages from a documentation site   | references/crawl.md      |

All commands support `-d` for full API request body (JSON5, `@file`, or stdin). Flags override `-d` fields.

## Configuration

When you need to manage site authentication (cookies, headers), cache TTL, or viewport defaults, read references/settings.md.

Common pattern: user provides cookies/auth for a site → store as site headers → all subsequent requests are authenticated:

```bash
ctx site set example.com Cookie "sid=abc; token=xyz"
```

## Self-Improvement

When you encounter friction — a command that doesn't work as expected, a misleading instruction in this skill, or confusing parameter design — read references/feedback.md and file a GitHub issue.
