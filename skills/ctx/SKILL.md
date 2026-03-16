---
name: ctx
description: >-
  Look up documentation for third-party libraries, frameworks, SDKs, APIs, CLI
  tools, and cloud services — search a documentation index by library name and
  query, then read full source documents (GitHub files, doc sites, JS-rendered
  SPAs) with section-level navigation for large pages.
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
| `https://...` (serves HTML)       | Jina Reader fallback → clean markdown                                    |
| `https://...` (JS/SPA page)       | `ctx read -f <url>` → Cloudflare Browser Rendering (full JS rendering) |

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
