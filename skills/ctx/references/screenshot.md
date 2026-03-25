# ctx screenshot

Capture a webpage as a PNG image with automatic page dimension awareness.

## Usage

```bash
ctx screenshot <url>
ctx screenshot <url> --full-page
ctx screenshot <url> --scroll 900        # capture second screen (at Y=900px)
ctx screenshot <url> --selector ".main-content" -o output.png
```

| Flag | Short | Default | Description |
|---|---|---|---|
| (positional) | | optional | URL (can also be in `-d` body) |
| `--output` | `-o` | auto-generated in cache dir | Output file path |
| `--full-page` | | false | Capture full page as single image |
| `--scroll` | | 0 | Scroll Y offset in pixels — captures a viewport-sized region at this offset |
| `--selector` | | | Screenshot only the element matching this CSS selector |
| `--no-cache` | | false | Bypass cache |
| `--data` | `-d` | | Full API request body (JSON5, `@file`, or `-` for stdin) |

Output: file path(s) on stdout, page metadata on last line (when multi-screen).

## Smart screenshot behavior

The command detects page length and automatically adapts:

| Page length | Behavior | Output example |
|---|---|---|
| 1 screen | Single image, no metadata | `/path.png` |
| 2–3 screens | Auto-split into separate per-screen images | `/path1.png [screen 1/3]`<br>`/path2.png [screen 2/3]`<br>`/path3.png [screen 3/3]`<br>`page=2700 viewport=900 screens=3` |
| >3 screens | First screen + navigation metadata | `/path.png`<br>`page=5400 viewport=900 screen=1/6 (--scroll 900 for next)` |

**Why split instead of one tall image?** LLM vision models down-sample overly tall images, turning detail into blurry pixel blocks. Separate per-screen images preserve full resolution.

Use `--scroll N` to navigate long pages. Subsequent `--scroll` calls reuse the cached full-page image (zero API cost).

## When to use

- Page has visual information that markdown can't capture (UI layouts, charts, diagrams)
- Need to verify how a rendered page looks
- Use `--scroll` to see content below the fold, guided by the metadata output

## Full API control

```bash
ctx screenshot -d '{
  url: "https://example.com",
  viewport: {width: 390, height: 844},
  screenshotOptions: {type: "jpeg", quality: 80}
}'
```

Flags (`--full-page`, `--scroll`, `--selector`, `-o`) override corresponding fields in `-d` body.
`--scroll` and `--full-page` are mutually exclusive.
