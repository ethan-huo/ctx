# ctx json

AI-powered structured data extraction from a webpage.

## Usage

```bash
ctx json <url> --prompt "Extract all API endpoints with their HTTP methods and parameters"
ctx json <url> --prompt "List all pricing tiers" --schema @schema.json
```

| Flag | Default | Description |
|---|---|---|
| (positional) | optional | URL (can also be in `-d` body) |
| `--prompt` | | Natural language prompt describing what to extract |
| `--schema` | | JSON schema file for response format (supports `@file`) |
| `-d` | | Full API request body (JSON5, `@file`, or stdin) |

Output: JSON to stdout.

If Cloudflare returns a 422 validation error but includes recoverable `rawAiResponse` data, `ctx json` prints that partial JSON to stdout and emits a warning on stderr instead of discarding it.

## When to use

- Need structured data from a page (pricing tables, API specs, product listings)
- CSS selectors alone aren't precise enough — AI understands semantics

## Prerequisites

The AI model must be configured in `~/.config/ctx/credentials.yaml` under the `ai:` section (model name + authorization header). See settings.md for the format.

## Schema enforcement

Use `--schema` to constrain output format to a JSON Schema:

```bash
ctx json <url> --prompt "Extract products" --schema @product-schema.json
```

## Full API control via -d

```bash
ctx json -d '{
  url: "https://example.com/pricing",
  prompt: "Extract all pricing tiers with features",
  response_format: {
    type: "json_schema",
    json_schema: {name: "pricing", schema: {type: "array", items: {type: "object"}}}
  }
}'
```
