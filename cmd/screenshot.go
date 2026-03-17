package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cache"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

type ScreenshotCmd struct {
	cfrender.DataFlag
	URL      string `arg:"" help:"URL to screenshot" optional:""`
	Output   string `short:"o" help:"Output file path (default: auto-generated)"`
	FullPage bool   `help:"Capture full page" default:"false"`
	Selector string `help:"Screenshot specific CSS selector element"`
	NoCache  bool   `help:"Bypass cache, always fetch fresh"`
}

func (c *ScreenshotCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	if c.URL != "" {
		overrides["url"] = c.URL
	}
	if c.FullPage {
		overrides["screenshotOptions"] = map[string]any{"fullPage": true}
	}
	if c.Selector != "" {
		overrides["selector"] = c.Selector
	}

	body, err := config.BuildRequestBody("screenshot", c.URL, dataBody, overrides)
	if err != nil {
		return err
	}

	if c.URL == "" && dataBody == nil {
		return fmt.Errorf("URL is required (as argument or in -d body)")
	}

	// Cache key includes the full request body so different params get different cache entries
	cacheKey := cache.Key("screenshot", string(body))

	if !c.NoCache {
		if data, _, ok := cache.Lookup(cacheKey, ".png"); ok {
			outPath := c.outputPath(cacheKey)
			if err := os.WriteFile(outPath, data, 0o644); err != nil {
				return err
			}
			fmt.Println(outPath)
			return nil
		}
	}

	client, err := cfrender.New()
	if err != nil {
		return err
	}

	data, err := client.Screenshot(context.Background(), c.URL, body)
	if err != nil {
		return fmt.Errorf("screenshot of %s failed: %w", c.URL, err)
	}

	_ = cache.Store(cacheKey, data, ".png", cache.Meta{
		URL:         c.URL,
		Source:      "cloudflare",
		ContentType: "image/png",
	})

	outPath := c.outputPath(cacheKey)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

func (c *ScreenshotCmd) outputPath(cacheKey string) string {
	if c.Output != "" {
		return c.Output
	}
	return cache.Path(cacheKey, ".png")
}
