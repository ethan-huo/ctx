package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type CrawlCmd struct {
	cfrender.DataFlag
	Target  string   `arg:"" help:"URL to crawl or job ID to check"`
	Limit   int      `help:"Max pages to crawl" default:"10"`
	Depth   int      `help:"Max link depth" default:"0"`
	Include []string `help:"URL include patterns (glob)"`
	Exclude []string `help:"URL exclude patterns (glob)"`
	NoWait  bool     `help:"Start crawl and return job ID without waiting" default:"false"`
	Cancel  bool     `help:"Cancel a running crawl job" default:"false"`
}

func (c *CrawlCmd) Run(_ *api.Client) error {
	client, err := cfrender.New()
	if err != nil {
		return err
	}

	ctx := context.Background()

	if uuidPattern.MatchString(c.Target) {
		return c.handleJobID(ctx, client, c.Target)
	}
	return c.handleURL(ctx, client, c.Target)
}

func (c *CrawlCmd) handleJobID(ctx context.Context, client *cfrender.Client, jobID string) error {
	if c.Cancel {
		if err := client.CrawlCancel(ctx, jobID); err != nil {
			return err
		}
		fmt.Println("Crawl job cancelled.")
		return nil
	}
	return c.pollAndPrint(ctx, client, jobID)
}

func (c *CrawlCmd) handleURL(ctx context.Context, client *cfrender.Client, url string) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	overrides["url"] = url
	overrides["limit"] = c.Limit
	overrides["formats"] = []string{"markdown"}

	if c.Depth > 0 {
		overrides["depth"] = c.Depth
	}

	opts := make(map[string]any)
	if len(c.Include) > 0 {
		opts["includePatterns"] = c.Include
	}
	if len(c.Exclude) > 0 {
		opts["excludePatterns"] = c.Exclude
	}
	if len(opts) > 0 {
		overrides["options"] = opts
	}

	body, err := config.BuildRequestBody("crawl", url, dataBody, overrides)
	if err != nil {
		return err
	}

	resp, err := client.CrawlStart(ctx, body)
	if err != nil {
		return err
	}

	jobID := resp.Result
	if c.NoWait {
		fmt.Println(jobID)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Crawl started: %s\n", jobID)
	return c.pollAndPrint(ctx, client, jobID)
}

func (c *CrawlCmd) pollAndPrint(ctx context.Context, client *cfrender.Client, jobID string) error {
	cursor := ""
	printed := make(map[string]bool)
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("crawl timed out after 10 minutes (job: %s)", jobID)
		}

		status, err := client.CrawlStatus(ctx, jobID, cursor)
		if err != nil {
			return err
		}

		for _, page := range status.Result.Pages {
			if printed[page.URL] || page.Markdown == "" {
				continue
			}
			printed[page.URL] = true
			if len(printed) > 1 {
				fmt.Println("\n---")
			}
			fmt.Printf("## %s\n%s", page.URL, page.Markdown)
		}

		cursor = status.Result.Cursor

		terminal := status.Status == "completed" ||
			status.Status == "cancelled_by_user" ||
			status.Status == "cancelled_due_to_timeout" ||
			status.Status == "cancelled_due_to_limits" ||
			status.Status == "errored"

		if terminal && cursor == "" {
			if len(printed) == 0 {
				fmt.Printf("Crawl completed but found no pages. Check the URL and try: ctx crawl %s --limit %d --depth 1\n", c.Target, c.Limit)
			} else if status.Status != "completed" {
				fmt.Printf("\n---\nCrawl ended: %s. ", status.Status)
				switch status.Status {
				case "cancelled_due_to_timeout":
					fmt.Printf("Try smaller --limit or --depth.\n")
				case "cancelled_due_to_limits":
					fmt.Printf("Increase --limit to crawl more pages.\n")
				default:
					fmt.Printf("Use `ctx crawl %s` to retry.\n", c.Target)
				}
			}
			return nil
		}

		if cursor == "" {
			fmt.Fprintf(os.Stderr, "Crawling... %d pages collected\n", len(printed))
			time.Sleep(2 * time.Second)
		}
	}
}
