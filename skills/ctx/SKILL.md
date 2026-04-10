---
name: ctx
description: >-
  Search and read library docs, GitHub-hosted content, read URLs as markdown; navigate long documents; and screenshot, link-scan, scrape, extract structured data from, or crawl websites.
---

## Execution rules

- `ctx` is a **global CLI tool**. Run it directly (`ctx read ...`), never `cd` into the skill directory first.
- **Always use `2>&1`** so stderr diagnostics (auto-retry messages, warnings) are visible.
- **Never pipe through `head`, `tail`, `cut`**, or any truncation — `ctx` already manages content length via structural summaries.
- **Never use `2>/dev/null`** — stderr carries actionable diagnostics, not noise.

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
| `--no-cache` |       | false    | Bypass cache, always fetch fresh                                          |
| `--toc`      |       | false    | Show heading outline with section numbers and line counts                 |
| `--section`  | `-s`  |          | Section(s) to extract (e.g. `1`, `1-3`, `1.2,3.1`)                        |
| `--comments` |       |          | GitHub issue comments to include (`1-3`, `7`, `all`)                      |
| `--data`     | `-d`  |          | CF API request body (JSON5, `@file`, or stdin)                            |

Auto-detects URL type and fetches accordingly:

- Local file / `file://` → direct read (always full content, no summary)
- `github://owner/repo@ref/path` → GitHub API (supports `@ref` for versioned docs)
- `https://github.com/.../blob/...` → GitHub API (auto-converted)
- `https://github.com/.../tree/<ref>/<path>` → GitHub directory listing via GitHub API (`[ctx:github-dir]` header + `ls`-style entries)
- `https://github.com/owner/repo` → repository README via GitHub API
- `https://github.com/owner/repo/issues/123` or `github://owner/repo/issues/123` → issue title/body/comments via GitHub API
- `https://www.youtube.com/watch?v=...`, `https://youtu.be/...`, `https://www.youtube.com/shorts/...` → transcript document via `yt-dlp` + caption fetch
- `https://...` (markdown/text/JSON/XML/YAML) → direct fetch
- `https://...` (HTML/SPA) → auto JS rendering fallback via Cloudflare

If `ctx read` returns `[ctx:github-dir]`, treat it as a navigable directory listing. Pick a child entry and re-run `ctx read` against that child path instead of trying to interpret it as prose.
If `ctx read` returns a long YouTube transcript, use `--toc` and `-s` exactly like markdown. The sections are time ranges or chapters, not prose headings.

GitHub issue reads are smart by default:

- `ctx read https://github.com/owner/repo/issues/123` returns title + body + as many comments as fit
- If comments exceed the budget, `ctx` appends a continuation hint like `ctx read github://owner/repo/issues/123 --comments 9-20`
- Use `--comments 1-3` for a precise range, or `--comments all` to force all comments

Prefer the default command first. Only add `--comments` when the issue is large or you need an exact slice.

When rendering via Cloudflare, a content-density heuristic automatically strips navigation, sidebar, header, and footer noise. This works for most sites without any configuration.

If the default cleanup selects the wrong content block on a specific site, use `-d` with `addScriptTag` to **replace** it with your own extraction logic:

```bash
ctx read -d '{url: "https://example.com", addScriptTag: [{content: "document.body.innerHTML = document.querySelector(\".doc-body\").outerHTML"}]}'
```

To find the right selector, probe the page first:

1. `ctx scrape <url> -s "main" -s "article" -s ".content"` — try common selectors, see which returns the content you need
2. Use the matched selector in `addScriptTag` to override the default cleanup

Other useful `-d` parameters: `cookies`, `waitForSelector`, `gotoOptions.waitUntil`, `viewport`.

**stdout/stderr contract**: stdout = document content, stderr = diagnostics (auto-retry, warnings). Both matter — see execution rules above.

### 3. Navigate large documents

Documents over 2000 lines produce a **structural summary** instead of the full content. The output starts with `[ctx:summary]` and shows every section heading with line counts and a preview:

```
[ctx:summary] 5000 lines, 12 sections. Read sections: ctx read <url> -s <number> (e.g. -s 10 or -s 10-20)
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

**Explore before acting.** When facing an unfamiliar site, start with `ctx read` or `ctx links` to understand what's there, then decide which tool fits.

| Situation                                | Tool                                     | Example                                                  |
| ---------------------------------------- | ---------------------------------------- | -------------------------------------------------------- |
| Need one page's full content             | `ctx read <url>`                         | Read a doc page                                          |
| Page is too long (>2000 lines)           | `ctx read <url> -s <section>`            | Navigate via structural summary                          |
| Only need specific elements from a page  | `ctx scrape <url> -s "selector"`         | Extract an API table, skip sidebar noise                 |
| Need content from many pages on one site | `ctx crawl <url> --limit N`              | Pull an entire docs section                              |
| Don't know which pages to read           | `ctx links <url>` then `ctx read`        | Explore site structure first                             |
| Need visual info (UI, charts, layouts)   | `ctx screenshot <url>`                   | Inspect rendered page; `--scroll 900` for below-the-fold |
| Screenshot only content, skip nav/ads    | `ctx screenshot <url> --selector "main"` | Common: `main`, `article`, `.content`, `#content`        |
| Need structured data extraction          | `ctx json <url> --prompt "..."`          | Extract pricing tiers as JSON                            |

### Fast SPA detection and fallback

`ctx read` returning empty or near-empty content is a strong signal that the page is JS-heavy, but **do not assume screenshot is the only viable fallback**. Distinguish between two cases:

1. **App/UI SPA with weak semantic DOM**: screenshots are often the fastest fallback.
2. **Docs/content SPA where JS rendering works but markdown extraction missed the main content block**: keep trying text-first tools before giving up.

Recommended workflow for text-heavy docs SPAs:

1. Try `ctx links <url>` first. If links render correctly, the page likely has a usable post-render DOM even if `ctx read` missed it.
2. Probe common content containers with `ctx scrape <url> -s "article" -s "main" -s ".markdown-body" -s ".content"`.
3. If scrape finds the right block, either use `ctx scrape` directly or retry `ctx read` with a targeted wait/extraction body:

```bash
ctx read -d '{
  url: "https://example.com/docs",
  waitForSelector: {selector: ".markdown-body"},
  gotoOptions: {waitUntil: "networkidle0"}
}'
```

4. If the target is structured content (pricing tables, API params, specs), try `ctx json` before falling back to screenshots.
5. Use `ctx screenshot` as the last resort when the rendered DOM is visually correct but not extractable as text.

Quick decision tree:

```
ctx read <url> → got useful content?
  YES → keep using read/scrape/json as needed
  NO  → try ctx links <url>
          links worked?
            YES → docs/content SPA → scrape common selectors, then retry read/json with waits
            NO  → app-heavy SPA → screenshot fallback
```

### Common compositions

- **Docs research**: `ctx docs <lib> "<query>"` → `ctx read <url>` → `ctx read <url> -s N` for deep sections
- **Full-site understanding**: `ctx crawl <url> --limit 20 --depth 2` (replaces manual links + read loop)
- **Surgical extraction**: `ctx read <url> --toc` to find target → `ctx scrape <url> -s "table.params"` to extract it
- **Content-focused screenshot** (semantic HTML sites only): `ctx scrape <url> -s "main" -s "article"` to find the right selector → `ctx screenshot <url> --selector "article"`

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
