package cmd

import (
	"strings"
	"testing"

	"github.com/ethan-huo/ctx/cfrender"
)

func TestRenderJSONOutput_NoData(t *testing.T) {
	stdout, stderr, err := renderJSONOutput("https://example.com/pricing", &cfrender.JSONResult{})
	if err != nil {
		t.Fatalf("renderJSONOutput returned error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "No data extracted from https://example.com/pricing") {
		t.Fatalf("stdout = %q, want no-data hint", stdout)
	}
}

func TestRenderJSONOutput_PartialArray(t *testing.T) {
	stdout, stderr, err := renderJSONOutput("https://example.com/pricing", &cfrender.JSONResult{
		Data: []any{
			map[string]any{"name": "pro", "price": 20},
		},
		Warning: "warning: extraction partially succeeded, results may be incomplete",
	})
	if err != nil {
		t.Fatalf("renderJSONOutput returned error: %v", err)
	}
	if !strings.Contains(stderr, "partially succeeded") {
		t.Fatalf("stderr = %q, want partial warning", stderr)
	}
	if !strings.Contains(stdout, "\"name\": \"pro\"") {
		t.Fatalf("stdout = %q, want rendered JSON array", stdout)
	}
}
