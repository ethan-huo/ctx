package cmd

import (
	"fmt"
	"strings"

	"github.com/ethan-huo/ctx/api"
)

type DocsCmd struct {
	Name  string `arg:"" help:"Library name or ID (e.g. 'mlx-swift' or '/ml-explore/mlx-swift')"`
	Query string `arg:"" help:"What to search for in the documentation"`
}

func (c *DocsCmd) Run(client *api.Client) error {
	libraryID := c.Name

	if !strings.HasPrefix(libraryID, "/") {
		libs, err := client.SearchLibraries(c.Name, c.Query)
		if err != nil {
			return err
		}
		if len(libs) == 0 {
			fmt.Printf("No libraries found for %q. Try: ctx search %s\n", c.Name, c.Name)
			return nil
		}
		libraryID = libs[0].ID
		fmt.Printf("# %s\n\n", libraryID)
	}

	resp, err := client.QueryDocs(libraryID, c.Query)
	if err != nil {
		return err
	}

	sources := extractSources(resp)
	if len(sources) == 0 {
		fmt.Printf("No documentation matched query %q in %s. Try a broader query.\n", c.Query, libraryID)
		return nil
	}

	// Items
	for i, src := range sources {
		fmt.Printf("%d. **%s**\n", i+1, src.title)
		for j, desc := range src.descriptions {
			if j >= 3 {
				break
			}
			fmt.Printf("   - %s\n", desc)
		}
	}

	// Hints
	fmt.Println("\n---\n")
	fmt.Println("Use `ctx read <url>` to fetch full documents:")
	for _, src := range sources {
		fmt.Printf("- %s\n", src.url)
	}

	return nil
}

type docSource struct {
	title        string
	descriptions []string
	url          string
}

func extractSources(resp *api.DocsResponse) []docSource {
	seen := map[string]int{}
	var sources []docSource

	add := func(rawURL, pageTitle, desc string) {
		if !isRealURL(rawURL) {
			return
		}
		url := normalizeURL(rawURL)
		if idx, ok := seen[url]; ok {
			if desc != "" {
				sources[idx].descriptions = append(sources[idx].descriptions, desc)
			}
			return
		}
		src := docSource{
			title: pageTitle,
			url:   url,
		}
		if desc != "" {
			src.descriptions = []string{desc}
		}
		seen[url] = len(sources)
		sources = append(sources, src)
	}

	for _, s := range resp.CodeSnippets {
		desc := s.CodeDescription
		if desc == "" {
			desc = s.CodeTitle
		}
		add(s.CodeID, s.PageTitle, desc)
	}
	for _, s := range resp.InfoSnippets {
		title := s.Breadcrumb
		if title == "" {
			title = "Info"
		}
		add(s.URL, title, "")
	}
	return sources
}

func isRealURL(u string) bool {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return false
	}
	if strings.Contains(u, "context7_") || strings.Contains(u, "context7.com/") {
		return false
	}
	return true
}

// normalizeURL converts GitHub blob URLs to github:// scheme,
// keeps everything else as-is.
func normalizeURL(rawURL string) string {
	if !strings.Contains(rawURL, "github.com") {
		return rawURL
	}

	trimmed := strings.TrimPrefix(rawURL, "https://github.com/")
	trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	parts := strings.SplitN(trimmed, "/blob/", 2)
	if len(parts) != 2 {
		return rawURL
	}
	repo := parts[0]
	rest := parts[1]
	// Strip branch prefix
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		rest = rest[idx+1:]
	} else {
		return rawURL
	}
	// Strip query/fragment
	if idx := strings.IndexByte(rest, '?'); idx >= 0 {
		rest = rest[:idx]
	}
	if idx := strings.IndexByte(rest, '#'); idx >= 0 {
		rest = rest[:idx]
	}
	return "github://" + repo + "/" + rest
}
