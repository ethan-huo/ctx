package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

type SiteCmd struct {
	Ls  SiteLsCmd  `cmd:"ls" help:"List configured domains or headers for a domain"`
	Set SiteSetCmd `cmd:"set" help:"Set header(s) for a domain"`
	Del SiteDelCmd `cmd:"del" help:"Delete a domain or a single header"`
}

// --- ls ---

type SiteLsCmd struct {
	Domain string `arg:"" help:"Domain to list headers for (omit to list all domains)" optional:""`
}

func (c *SiteLsCmd) Run(_ *api.Client) error {
	creds, err := config.LoadCredentials()
	if err != nil || len(creds.Sites) == 0 {
		fmt.Println("No sites configured.")
		return nil
	}

	if c.Domain == "" {
		domains := make([]string, 0, len(creds.Sites))
		for d := range creds.Sites {
			domains = append(domains, d)
		}
		sort.Strings(domains)
		for _, d := range domains {
			n := len(creds.Sites[d].Headers)
			fmt.Printf("%s  (%d headers)\n", d, n)
		}
		if len(domains) > 0 {
			fmt.Printf("\nUse `ctx site ls <domain>` to see headers for a specific domain.\n")
		}
		return nil
	}

	site, ok := creds.Sites[c.Domain]
	if !ok || len(site.Headers) == 0 {
		fmt.Printf("No headers configured for %s. Set one with: ctx site set %s <header-name> <value>\n", c.Domain, c.Domain)
		return nil
	}

	keys := make([]string, 0, len(site.Headers))
	for k := range site.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %s\n", k, site.Headers[k])
	}
	return nil
}

// --- set ---

type SiteSetCmd struct {
	Domain string `arg:"" help:"Domain (e.g. example.com)"`
	Key    string `arg:"" help:"Header name, or @file for bulk JSON import" optional:""`
	Value  string `arg:"" help:"Header value (supports @file)" optional:""`
}

func (c *SiteSetCmd) Run(_ *api.Client) error {
	// Bulk mode: ctx site set example.com @headers.json5
	if strings.HasPrefix(c.Domain, "@") {
		return fmt.Errorf("first argument must be a domain, not a file reference")
	}

	if c.Key == "" {
		return fmt.Errorf("header name or @file is required")
	}

	// Bulk import: key starts with @
	if strings.HasPrefix(c.Key, "@") {
		return c.bulkSet()
	}

	// Single header
	val, err := cfrender.ResolveValue(c.Value)
	if err != nil {
		return fmt.Errorf("reading value: %w", err)
	}

	err = config.UpdateCredentials(func(creds *config.Credentials) {
		if creds.Sites == nil {
			creds.Sites = make(map[string]config.Site)
		}
		site := creds.Sites[c.Domain]
		if site.Headers == nil {
			site.Headers = make(map[string]string)
		}
		site.Headers[c.Key] = val
		creds.Sites[c.Domain] = site
	})
	if err != nil {
		return err
	}
	fmt.Printf("Set %s for %s.\n", c.Key, c.Domain)
	return nil
}

func (c *SiteSetCmd) bulkSet() error {
	data, err := cfrender.ResolveValue(c.Key)
	if err != nil {
		return fmt.Errorf("reading bulk file: %w", err)
	}

	var headers map[string]string
	if err := json.Unmarshal([]byte(data), &headers); err != nil {
		return fmt.Errorf("invalid JSON in bulk file: %w", err)
	}

	err = config.UpdateCredentials(func(creds *config.Credentials) {
		if creds.Sites == nil {
			creds.Sites = make(map[string]config.Site)
		}
		site := creds.Sites[c.Domain]
		if site.Headers == nil {
			site.Headers = make(map[string]string)
		}
		for k, v := range headers {
			site.Headers[k] = v
		}
		creds.Sites[c.Domain] = site
	})
	if err != nil {
		return err
	}
	fmt.Printf("Imported %d headers for %s.\n", len(headers), c.Domain)
	return nil
}

// --- del ---

type SiteDelCmd struct {
	Domain string `arg:"" help:"Domain to delete (or delete a single header)"`
	Key    string `arg:"" help:"Header name to delete (omit to delete entire domain)" optional:""`
}

func (c *SiteDelCmd) Run(_ *api.Client) error {
	return config.UpdateCredentials(func(creds *config.Credentials) {
		if creds.Sites == nil {
			return
		}
		if c.Key == "" {
			delete(creds.Sites, c.Domain)
			fmt.Printf("Deleted all headers for %s.\n", c.Domain)
			return
		}
		site := creds.Sites[c.Domain]
		delete(site.Headers, c.Key)
		if len(site.Headers) == 0 {
			delete(creds.Sites, c.Domain)
		} else {
			creds.Sites[c.Domain] = site
		}
		fmt.Printf("Deleted %s for %s.\n", c.Key, c.Domain)
	})
}
