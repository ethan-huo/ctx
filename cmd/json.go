package cmd

import (
	"context"
	"encoding/json"
	"fmt"

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

	result, err := client.JSON(context.Background(), c.URL, body)
	if err != nil {
		return fmt.Errorf("json extraction from %s failed: %w\nHint: ensure AI model is configured in ~/.config/ctx/credentials.yaml under 'ai:' section.", c.URL, err)
	}

	if len(result) == 0 {
		fmt.Printf("No data extracted from %s. Try a more specific --prompt or inspect the page with `ctx read %s` first.\n", c.URL, c.URL)
		return nil
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
