# ctx Configuration Reference

ctx uses two configuration files in `~/.config/ctx/`:

## credentials.yaml — Secrets & Per-Site Headers

```yaml
# Cloudflare Browser Rendering credentials
cloudflare:
  account_id: your-account-id
  api_token: your-api-token

# Context7 OAuth tokens (managed by ctx auth login ctx7)
ctx7:
  access_token: eyJ...
  refresh_token: dGhp...
  expires_at: 1742428800000

# AI model for ctx json (auto-injected into /json requests)
ai:
  model: anthropic/claude-sonnet-4-20250514
  authorization: Bearer sk-ant-api03-...

# Per-domain headers (auto-injected into all CF requests matching the domain)
sites:
  example.com:
    headers:
      Authorization: Bearer token123
      Cookie: "sid=abc; token=xyz"
  internal.corp.com:
    headers:
      X-Api-Key: secret-key-here
```

### Managing site headers via CLI

```bash
# List all configured domains
ctx site ls

# List headers for a domain
ctx site ls example.com

# Set a single header
ctx site set example.com Authorization "Bearer token123"

# Set a header value from file
ctx site set example.com Cookie @/tmp/cookie.txt

# Bulk import headers from JSON5 file
ctx site set example.com @headers.json5

# Delete a single header
ctx site del example.com Authorization

# Delete all headers for a domain
ctx site del example.com
```

## settings.jsonc — Behavioral Defaults

```jsonc
{
  // Default parameters merged into all Cloudflare Browser Rendering requests
  "defaults": {
    "gotoOptions": {"waitUntil": "networkidle2"},
    "viewport": {"width": 1920, "height": 1080}
  }
}
```

## Request Body Merge Order

When a CF command executes, the request body is built by merging these layers (lowest → highest priority):

1. `settings.jsonc` defaults
2. Site headers for matching domain → `setExtraHTTPHeaders`
3. AI credentials → `custom_ai` (only for `ctx json`)
4. `-d` body (inline JSON5, @file, or stdin)
5. CLI flags

Example: if `settings.jsonc` has `viewport: {width: 1920}` and `-d` has `viewport: {width: 390}`, the request uses `390`.
