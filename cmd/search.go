package cmd

import (
	"fmt"

	"github.com/ethan-huo/ctx/api"
)

type SearchCmd struct {
	Name  string `arg:"" help:"Library name to search for"`
	Query string `arg:"" optional:"" help:"Query for relevance ranking"`
}

func (c *SearchCmd) Run(client *api.Client) error {
	libs, err := client.SearchLibraries(c.Name, c.Query)
	if err != nil {
		return err
	}
	if len(libs) == 0 {
		fmt.Printf("No libraries found for %q. Try a more specific name or check spelling.\n", c.Name)
		return nil
	}

	for i, lib := range libs {
		if i >= 5 {
			break
		}
		fmt.Printf("%d. %s  %s\n", i+1, lib.ID, lib.Title)
		if lib.Description != "" {
			desc := lib.Description
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			fmt.Printf("   %s\n", desc)
		}
		fmt.Printf("   snippets:%d  score:%.0f  stars:%d\n\n",
			lib.TotalSnippets, lib.BenchmarkScore, lib.Stars)
	}
	fmt.Println("Next: ctx docs <library-name> \"<query>\" to find relevant documentation.")
	return nil
}
