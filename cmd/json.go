package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

type JSONCmd struct {
	cfrender.DataFlag
	URL    string `arg:"" help:"URL to extract data from" optional:""`
	Prompt string `help:"Natural language prompt describing what to extract"`
	Schema string `help:"JSON schema file for response format (@file supported)"`
}

func (c *JSONCmd) Run(_ *api.Client) error {
	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	if c.URL != "" {
		overrides["url"] = c.URL
	}
	if c.Prompt != "" {
		overrides["prompt"] = c.Prompt
	}
	if c.Schema != "" {
		schemaStr, err := cfrender.ResolveValue(c.Schema)
		if err != nil {
			return fmt.Errorf("reading schema: %w", err)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
			return fmt.Errorf("invalid JSON schema: %w", err)
		}
		overrides["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": schema,
		}
	}

	// BuildRequestBody auto-injects ai.custom_ai from credentials for "json" endpoint
	body, err := config.BuildRequestBody("json", c.URL, dataBody, overrides)
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

	url := effectiveURL(c.URL, body)

	result, err := client.JSON(context.Background(), c.URL, body)
	if err != nil {
		return fmt.Errorf("json extraction from %s failed: %w\nHint: ensure AI model is configured in ~/.config/ctx/credentials.yaml under 'ai:' section.", url, err)
	}

	stdout, stderr, err := renderJSONOutput(url, result)
	if err != nil {
		return err
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	fmt.Print(stdout)
	return nil
}

func renderJSONOutput(url string, result *cfrender.JSONResult) (string, string, error) {
	var stderr string
	if result != nil && result.Warning != "" {
		// Partial success should still stream structured data to stdout for agents.
		stderr = result.Warning + "\n"
	}
	if result == nil || result.Data == nil {
		return fmt.Sprintf("No data extracted from %s. Try a more specific --prompt or inspect the page with `ctx read %s` first.\n", url, url), stderr, nil
	}

	out, err := json.MarshalIndent(result.Data, "", "  ")
	if err != nil {
		return "", stderr, err
	}
	return string(out) + "\n", stderr, nil
}
