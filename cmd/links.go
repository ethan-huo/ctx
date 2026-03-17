package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cache"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

type LinksCmd struct {
	cfrender.DataFlag
	URL          string `arg:"" help:"URL to extract links from" optional:""`
	VisibleOnly  bool   `help:"Only visible links" default:"false"`
	InternalOnly bool   `help:"Exclude external domain links" default:"false"`
	NoCache      bool   `help:"Bypass cache, always fetch fresh"`
}

func (c *LinksCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	if c.URL != "" {
		overrides["url"] = c.URL
	}
	if c.VisibleOnly {
		overrides["visibleLinksOnly"] = true
	}
	if c.InternalOnly {
		overrides["excludeExternalLinks"] = true
	}

	body, err := config.BuildRequestBody("links", c.URL, dataBody, overrides)
	if err != nil {
		return err
	}

	if c.URL == "" && dataBody == nil {
		return fmt.Errorf("URL is required (as argument or in -d body)")
	}

	cacheKey := cache.Key("links", string(body))

	if !c.NoCache {
		if data, _, ok := cache.Lookup(cacheKey, ".txt"); ok {
			fmt.Print(string(data))
			return nil
		}
	}

	client, err := cfrender.New()
	if err != nil {
		return err
	}

	links, err := client.Links(context.Background(), c.URL, body)
	if err != nil {
		return fmt.Errorf("links extraction from %s failed: %w", c.URL, err)
	}

	if len(links) == 0 {
		fmt.Printf("No links found on %s. The page may require JavaScript or have no anchor elements.\n", c.URL)
		return nil
	}

	output := strings.Join(links, "\n") + "\n"
	_ = cache.Store(cacheKey, []byte(output), ".txt", cache.Meta{
		URL:    c.URL,
		Source: "cloudflare",
	})

	fmt.Print(output)
	fmt.Printf("\n---\n%d links found. Use `ctx read <url>` to read any of them.\n", len(links))
	return nil
}
