package cfrender

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseJSONHTTPResponse_Success(t *testing.T) {
	result, err := parseJSONHTTPResponse(http.StatusOK, []byte(`{
		"success": true,
		"result": {"plans": [{"name": "pro", "price": 20}]}
	}`))
	if err != nil {
		t.Fatalf("parseJSONHTTPResponse returned error: %v", err)
	}
	if result.Warning != "" {
		t.Fatalf("unexpected warning: %q", result.Warning)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("result.Data should be an object, got %T", result.Data)
	}
	plans, ok := data["plans"].([]any)
	if !ok || len(plans) != 1 {
		t.Fatalf("plans = %#v, want one entry", data["plans"])
	}
}

func TestParseJSONHTTPResponse_Partial422UsesRawAIResponse(t *testing.T) {
	result, err := parseJSONHTTPResponse(http.StatusUnprocessableEntity, []byte(`{
		"success": false,
		"errors": [{"message": "Unable to form JSON based on user prompt"}],
		"rawAiResponse": "[{\"instance_type\":\"ecs.g3i.large\",\"price\":0.0811}]"
	}`))
	if err != nil {
		t.Fatalf("parseJSONHTTPResponse returned error: %v", err)
	}
	if !strings.Contains(result.Warning, "partially succeeded") {
		t.Fatalf("warning = %q, want partial-success warning", result.Warning)
	}

	rows, ok := result.Data.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("result.Data = %#v, want one parsed row", result.Data)
	}
}

func TestParseJSONHTTPResponse_422WithoutUsableRawAIResponseFails(t *testing.T) {
	_, err := parseJSONHTTPResponse(http.StatusUnprocessableEntity, []byte(`{
		"success": false,
		"errors": [{"message": "Unable to form JSON based on user prompt"}],
		"rawAiResponse": "[{broken json]"
	}`))
	if err == nil {
		t.Fatal("expected error when rawAiResponse is not valid JSON")
	}
}
