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
make install
```

Requires Go 1.24+.

## Usage

```bash
# Find documentation sources for a library
ctx docs mlx-swift "GPU stream thread safety"
ctx docs sparkle "appcast auto update"
ctx docs convex "Swift client authentication"

# Read a full document
ctx read github://ml-explore/mlx-swift/Source/MLX/Documentation.docc/MLXArray.md
ctx read https://sparkle-project.org/documentation/index

# Find a library by name
ctx search react-native
```

## Authentication

ctx shares credentials with ctx7 (`~/.context7/credentials.json`).

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
| `github://owner/repo/path` | GitHub Contents API |
| `https://github.com/.../blob/...` | Auto-converted to GitHub API |
| Any `https://` | `Accept: text/markdown` negotiation → Jina Reader fallback |

## AI Agent Integration

The `skills/ctx/` directory contains a SKILL.md for Claude Code / Cursor / similar tools. Install it with your agent's skill mechanism.
