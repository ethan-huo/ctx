# ctx scrape

Extract specific elements from a webpage by CSS selector.

## Usage

```bash
ctx scrape <url> -s "h1" -s "p.description"
ctx scrape <url> -s "table.api-params" --text-only
```

| Flag | Short | Default | Description |
|---|---|---|---|
| (positional) | | optional | URL (can also be in `-d` body) |
| `--selector` | `-s` | | CSS selectors (repeatable: `-s "h1" -s "table"`) |
| `--text-only` | | false | Output plain text instead of JSON |
| `--data` | `-d` | | Full API request body (JSON5, `@file`, or `-` for stdin) |

When using `-d`, selectors go in the body as `elements` array — no `-s` flags needed.

## When to use

- Extract specific parts of a page (API tables, code blocks, pricing) without full-page markdown
- `ctx read` returns too much — scrape is surgical

## Output format

Default stdout is pretty JSON:
```json
[
  {
    "selector": "h1",
    "results": [
      {
        "text": "API Reference",
        "html": "<h1>API Reference</h1>",
        "attributes": {
          "id": "api-reference"
        },
        "width": 800,
        "height": 40
      }
    ]
  }
]
```

Structure:

- Top level: array of selector groups, one object per requested selector
- `.[] .selector`: the CSS selector that produced this group
- `.[] .results`: matched elements for that selector
- `.[] .results[] .text`: visible text content
- `.[] .results[] .html`: matched element HTML; cleaned by default, raw with `--raw`
- `.[] .results[] .attributes`: object of element attributes, omitted when empty
- `.[] .results[] .width` / `.height`: rendered element dimensions in pixels

With `--text-only`, stdout is not JSON: it prints one text value per line.

If no elements match, stdout is a human-readable diagnostic rather than JSON. When scripting, prefer selectors that are expected to match or guard JSON parsing accordingly.

## jq examples

Print every matched text value:

```bash
ctx scrape <url> -s "h1" -s "table.api-params" | jq -r '.[].results[].text'
```

Print only the text for a specific selector:

```bash
ctx scrape <url> -s "h1" -s "table.api-params" \
  | jq -r '.[] | select(.selector == "table.api-params") | .results[].text'
```

Extract links from matched anchors:

```bash
ctx scrape <url> -s "main a" \
  | jq -r '.[].results[] | select(.attributes.href) | [.text, .attributes.href] | @tsv'
```

## Full API control via -d

```bash
ctx scrape -d '{
  url: "https://example.com",
  elements: [{selector: "h1"}, {selector: "table.pricing"}]
}'
```
