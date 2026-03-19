package cfrender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/browser_rendering"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/ethan-huo/ctx/api"
)

// Client wraps the Cloudflare SDK for Browser Rendering operations.
type Client struct {
	inner     *cloudflare.Client
	accountID string
	apiToken  string
}

// New creates a Client from stored credentials (~/.config/ctx/cloudflare.json).
func New() (*Client, error) {
	creds, err := api.LoadCFCredentials()
	if err != nil {
		return nil, fmt.Errorf("cloudflare not configured — run `ctx auth login cloudflare` first")
	}
	inner := cloudflare.NewClient(option.WithAPIToken(creds.APIToken))
	return &Client{inner: inner, accountID: creds.AccountID, apiToken: creds.APIToken}, nil
}

// Validate checks that credentials work by hitting the markdown endpoint.
func (c *Client) Validate() error {
	_, err := c.Markdown(context.Background(), "https://example.com", nil)
	return err
}

// Markdown fetches a URL's content as markdown.
func (c *Client) Markdown(ctx context.Context, url string, body []byte) (string, error) {
	params := browser_rendering.MarkdownNewParams{
		AccountID: cloudflare.F(c.accountID),
	}
	var opts []option.RequestOption
	if body != nil {
		opts = append(opts, option.WithRequestBody("application/json", body))
	} else {
		params.Body = browser_rendering.MarkdownNewParamsBodyObject{
			URL: cloudflare.F(url),
			GotoOptions: cloudflare.F(browser_rendering.MarkdownNewParamsBodyObjectGotoOptions{
				WaitUntil: cloudflare.F[browser_rendering.MarkdownNewParamsBodyObjectGotoOptionsWaitUntilUnion](
					browser_rendering.MarkdownNewParamsBodyObjectGotoOptionsWaitUntilString(
						browser_rendering.MarkdownNewParamsBodyObjectGotoOptionsWaitUntilStringNetworkidle2,
					),
				),
			}),
		}
	}
	result, err := c.inner.BrowserRendering.Markdown.New(ctx, params, opts...)
	if err != nil {
		return "", fmt.Errorf("cloudflare markdown: %w", err)
	}
	if result == nil {
		return "", nil
	}
	return *result, nil
}

// Screenshot captures a webpage as a PNG image via direct HTTP call.
// The CF SDK doesn't handle binary responses well, so we bypass it.
func (c *Client) Screenshot(ctx context.Context, url string, body []byte) ([]byte, error) {
	if body == nil {
		b, _ := json.Marshal(map[string]any{"url": url})
		body = b
	}
	resp, err := c.cfHTTP(ctx, "POST", "screenshot", body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare screenshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudflare screenshot HTTP %d: %s", resp.StatusCode, data)
	}
	return io.ReadAll(resp.Body)
}

// Links extracts all links from a webpage.
func (c *Client) Links(ctx context.Context, url string, body []byte) ([]string, error) {
	params := browser_rendering.LinkNewParams{
		AccountID: cloudflare.F(c.accountID),
	}
	var opts []option.RequestOption
	if body != nil {
		opts = append(opts, option.WithRequestBody("application/json", body))
	} else {
		params.Body = browser_rendering.LinkNewParamsBody{
			URL: cloudflare.F(url),
		}
	}

	result, err := c.inner.BrowserRendering.Links.New(ctx, params, opts...)
	if err != nil {
		return nil, fmt.Errorf("cloudflare links: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return *result, nil
}

// ScrapeResult holds results for one CSS selector.
type ScrapeResult struct {
	Selector string             `json:"selector"`
	Results  []ScrapeElementHit `json:"results"`
}

// ScrapeElementHit is a single matched element.
type ScrapeElementHit struct {
	Text       string            `json:"text"`
	HTML       string            `json:"html"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Width      float64           `json:"width"`
	Height     float64           `json:"height"`
}

// Scrape extracts elements from a webpage by CSS selectors.
// Bypasses the SDK because it mismodels the CF response (results is an array, not a struct).
func (c *Client) Scrape(ctx context.Context, url string, selectors []string, body []byte) ([]ScrapeResult, error) {
	if body == nil {
		elements := make([]map[string]string, len(selectors))
		for i, s := range selectors {
			elements[i] = map[string]string{"selector": s}
		}
		b, _ := json.Marshal(map[string]any{"url": url, "elements": elements})
		body = b
	}

	resp, err := c.cfHTTP(ctx, "POST", "scrape", body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare scrape: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cloudflare scrape HTTP %d: %s", resp.StatusCode, data)
	}

	var cfResp struct {
		Success bool `json:"success"`
		Result  []struct {
			Selector string `json:"selector"`
			Results  []struct {
				Text       string  `json:"text"`
				HTML       string  `json:"html"`
				Width      float64 `json:"width"`
				Height     float64 `json:"height"`
				Attributes []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"attributes"`
			} `json:"results"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &cfResp); err != nil {
		return nil, fmt.Errorf("invalid scrape response: %w", err)
	}

	out := make([]ScrapeResult, len(cfResp.Result))
	for i, entry := range cfResp.Result {
		sr := ScrapeResult{Selector: entry.Selector}
		for _, r := range entry.Results {
			attrs := make(map[string]string)
			for _, a := range r.Attributes {
				attrs[a.Name] = a.Value
			}
			sr.Results = append(sr.Results, ScrapeElementHit{
				Text:       r.Text,
				HTML:       r.HTML,
				Attributes: attrs,
				Width:      r.Width,
				Height:     r.Height,
			})
		}
		out[i] = sr
	}
	return out, nil
}

// JSON extracts structured data from a webpage using AI.
func (c *Client) JSON(ctx context.Context, url string, body []byte) (map[string]any, error) {
	params := browser_rendering.JsonNewParams{
		AccountID: cloudflare.F(c.accountID),
	}
	var opts []option.RequestOption
	if body != nil {
		opts = append(opts, option.WithRequestBody("application/json", body))
	} else {
		params.Body = browser_rendering.JsonNewParamsBody{
			URL: cloudflare.F(url),
		}
	}

	result, err := c.inner.BrowserRendering.Json.New(ctx, params, opts...)
	if err != nil {
		return nil, fmt.Errorf("cloudflare json: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return *result, nil
}

// --- Crawl (no SDK support, direct HTTP) ---

type CrawlStartResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result"`
}

type CrawlPage struct {
	URL      string `json:"url"`
	Status   string `json:"status"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
}

type CrawlStatusResponse struct {
	Success bool `json:"success"`
	Result  struct {
		Status   string      `json:"status"`
		Pages    []CrawlPage `json:"records"`
		Cursor   int         `json:"cursor"`
		Total    int         `json:"total"`
		Finished int         `json:"finished"`
	} `json:"result"`
}

var cfHTTPClient = &http.Client{Timeout: 120 * time.Second}

func (c *Client) cfHTTP(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/browser-rendering/%s", c.accountID, path)

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return cfHTTPClient.Do(req)
}

func (c *Client) CrawlStart(ctx context.Context, body []byte) (*CrawlStartResponse, error) {
	resp, err := c.cfHTTP(ctx, "POST", "crawl", body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare crawl start: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cloudflare crawl HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result CrawlStartResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid crawl response: %w", err)
	}
	return &result, nil
}

func (c *Client) CrawlStatus(ctx context.Context, jobID string, cursor int) (*CrawlStatusResponse, error) {
	path := "crawl/" + jobID
	if cursor > 0 {
		path += fmt.Sprintf("?cursor=%d", cursor)
	}
	resp, err := c.cfHTTP(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("cloudflare crawl status: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cloudflare crawl status HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result CrawlStatusResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid crawl status response: %w", err)
	}
	return &result, nil
}

func (c *Client) CrawlCancel(ctx context.Context, jobID string) error {
	resp, err := c.cfHTTP(ctx, "DELETE", "crawl/"+jobID, nil)
	if err != nil {
		return fmt.Errorf("cloudflare crawl cancel: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare crawl cancel HTTP %d: %s", resp.StatusCode, string(data))
	}
	return nil
}
