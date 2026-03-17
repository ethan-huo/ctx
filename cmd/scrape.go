package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

type ScrapeCmd struct {
	cfrender.DataFlag
	URL      string   `arg:"" help:"URL to scrape" optional:""`
	Selector []string `short:"s" help:"CSS selectors (repeatable)" required:""`
	TextOnly bool     `help:"Output text content only" default:"false"`
}

func (c *ScrapeCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	if c.URL != "" {
		overrides["url"] = c.URL
	}
	if len(c.Selector) > 0 {
		elements := make([]map[string]string, len(c.Selector))
		for i, s := range c.Selector {
			elements[i] = map[string]string{"selector": s}
		}
		overrides["elements"] = elements
	}

	body, err := config.BuildRequestBody("scrape", c.URL, dataBody, overrides)
	if err != nil {
		return err
	}

	if c.URL == "" && dataBody == nil {
		return fmt.Errorf("URL is required (as argument or in -d body)")
	}

	client, err := cfrender.New()
	if err != nil {
		return err
	}

	results, err := client.Scrape(context.Background(), c.URL, c.Selector, body)
	if err != nil {
		return fmt.Errorf("scrape of %s with selectors %v failed: %w", c.URL, c.Selector, err)
	}

	allEmpty := true
	for _, sr := range results {
		if len(sr.Results) > 0 {
			allEmpty = false
			break
		}
	}
	if allEmpty {
		fmt.Printf("No elements matched selectors %v on %s.\n", c.Selector, c.URL)
		fmt.Printf("Hint: use `ctx read %s` to inspect page content, or `ctx screenshot %s` to see the rendered page.\n", c.URL, c.URL)
		return nil
	}

	if c.TextOnly {
		for _, sr := range results {
			for _, hit := range sr.Results {
				fmt.Println(hit.Text)
			}
		}
		return nil
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
