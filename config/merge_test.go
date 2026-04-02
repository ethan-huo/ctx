package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// ===== deepMerge =====

func TestDeepMerge_FlatOverride(t *testing.T) {
	dst := map[string]any{"a": "1", "b": "2"}
	src := map[string]any{"b": "3", "c": "4"}
	deepMerge(dst, src)

	if dst["a"] != "1" {
		t.Errorf("a = %v, want 1", dst["a"])
	}
	if dst["b"] != "3" {
		t.Errorf("b = %v, want 3 (overridden)", dst["b"])
	}
	if dst["c"] != "4" {
		t.Errorf("c = %v, want 4 (added)", dst["c"])
	}
}

func TestDeepMerge_NestedMerge(t *testing.T) {
	dst := map[string]any{
		"viewport": map[string]any{"width": 1920, "height": 1080},
	}
	src := map[string]any{
		"viewport": map[string]any{"width": 390},
	}
	deepMerge(dst, src)

	vp := dst["viewport"].(map[string]any)
	if vp["width"] != 390 {
		t.Errorf("width = %v, want 390 (overridden)", vp["width"])
	}
	if vp["height"] != 1080 {
		t.Errorf("height = %v, want 1080 (preserved)", vp["height"])
	}
}

func TestDeepMerge_NonMapOverridesMap(t *testing.T) {
	dst := map[string]any{
		"viewport": map[string]any{"width": 1920},
	}
	src := map[string]any{
		"viewport": "fullscreen",
	}
	deepMerge(dst, src)

	if dst["viewport"] != "fullscreen" {
		t.Errorf("viewport = %v, want fullscreen (non-map overrides map)", dst["viewport"])
	}
}

func TestDeepMerge_MapOverridesNonMap(t *testing.T) {
	dst := map[string]any{
		"viewport": "fullscreen",
	}
	src := map[string]any{
		"viewport": map[string]any{"width": 390},
	}
	deepMerge(dst, src)

	vp, ok := dst["viewport"].(map[string]any)
	if !ok {
		t.Fatalf("viewport should be map, got %T", dst["viewport"])
	}
	if vp["width"] != 390 {
		t.Errorf("width = %v, want 390", vp["width"])
	}
}

// ===== extractDomain =====

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/page", "example.com"},
		{"https://sub.example.com:8080/page", "sub.example.com"},
		{"http://localhost:3000", "localhost"},
		{"not-a-url", ""},
	}

	for _, tt := range tests {
		got := extractDomain(tt.input)
		if got != tt.want {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ===== BuildRequestBody — effectiveURL resolution =====

// Helper: unmarshal JSON to map for assertions.
func mustUnmarshalBody(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return m
}

func TestBuildRequestBody_FlagOverridesWin(t *testing.T) {
	dataBody := []byte(`{"url":"http://from-data.com","timeout":5}`)
	overrides := map[string]any{"url": "http://from-flag.com"}

	body, err := BuildRequestBody("markdown", "", dataBody, overrides)
	if err != nil {
		t.Fatal(err)
	}

	m := mustUnmarshalBody(t, body)
	if m["url"] != "http://from-flag.com" {
		t.Errorf("url = %v, want http://from-flag.com (flag wins)", m["url"])
	}
	// data body fields preserved
	if m["timeout"] != float64(5) {
		t.Errorf("timeout = %v, want 5 (preserved from -d)", m["timeout"])
	}
}

func TestBuildRequestBody_EffectiveURL_FromFlags(t *testing.T) {
	// When targetURL is empty, effectiveURL should be resolved from flagOverrides
	overrides := map[string]any{"url": "http://from-flag.com"}
	body, err := BuildRequestBody("markdown", "", nil, overrides)
	if err != nil {
		t.Fatal(err)
	}

	m := mustUnmarshalBody(t, body)
	if m["url"] != "http://from-flag.com" {
		t.Errorf("url = %v, want http://from-flag.com", m["url"])
	}
}

func TestBuildRequestBody_EffectiveURL_FromDataBody(t *testing.T) {
	// When targetURL and flagOverrides are empty, effectiveURL from -d body
	dataBody := []byte(`{"url":"http://from-data.com"}`)
	body, err := BuildRequestBody("markdown", "", dataBody, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := mustUnmarshalBody(t, body)
	if m["url"] != "http://from-data.com" {
		t.Errorf("url = %v, want http://from-data.com", m["url"])
	}
}

func TestBuildRequestBody_DataBodyDeepMerge(t *testing.T) {
	dataBody := []byte(`{"gotoOptions":{"waitUntil":"load"}}`)
	overrides := map[string]any{
		"gotoOptions": map[string]any{"timeout": 5000},
	}

	body, err := BuildRequestBody("markdown", "http://example.com", dataBody, overrides)
	if err != nil {
		t.Fatal(err)
	}

	m := mustUnmarshalBody(t, body)
	opts := m["gotoOptions"].(map[string]any)
	// -d body has waitUntil, flags add timeout — both should be present
	if opts["waitUntil"] != "load" {
		t.Errorf("waitUntil = %v, want load", opts["waitUntil"])
	}
	if opts["timeout"] != float64(5000) {
		t.Errorf("timeout = %v, want 5000", opts["timeout"])
	}
}

func TestBuildRequestBody_InvalidJSON(t *testing.T) {
	_, err := BuildRequestBody("markdown", "", []byte("not json"), nil)
	if err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

func TestBuildRequestBody_NilBodyAndOverrides(t *testing.T) {
	body, err := BuildRequestBody("markdown", "http://example.com", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should produce valid JSON (possibly just {})
	m := mustUnmarshalBody(t, body)
	_ = m // no panic = success
}

func TestBuildRequestBody_MarkdownDefaultCleanup(t *testing.T) {
	body, err := BuildRequestBody("markdown", "http://example.com", nil, map[string]any{"url": "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	m := mustUnmarshalBody(t, body)
	scripts, ok := m["addScriptTag"].([]any)
	if !ok || len(scripts) == 0 {
		t.Fatal("markdown endpoint should have default addScriptTag")
	}
	tag := scripts[0].(map[string]any)
	content, ok := tag["content"].(string)
	if !ok || content == "" {
		t.Fatal("cleanup script should be non-empty embedded content")
	}
	// Verify all three phases are present in the embedded script.
	// Detailed DOM behavior is tested in cleanup.spec.ts (BDD specs).
	for _, marker := range []string{
		`querySelector("[role=main]")`, // Phase 1: semantic containers
		"bodyLen",                      // Phase 2: coverage guard
		"remove()",                     // Phase 3: subtractive fallback
	} {
		if !strings.Contains(content, marker) {
			t.Errorf("embedded cleanup.js missing phase marker: %s", marker)
		}
	}
}

func TestBuildRequestBody_MarkdownDefaultGotoOptions(t *testing.T) {
	body, err := BuildRequestBody("markdown", "http://example.com", nil, map[string]any{"url": "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}

	m := mustUnmarshalBody(t, body)
	opts, ok := m["gotoOptions"].(map[string]any)
	if !ok {
		t.Fatal("markdown endpoint should have default gotoOptions")
	}
	if opts["waitUntil"] != "networkidle2" {
		t.Errorf("waitUntil = %v, want networkidle2", opts["waitUntil"])
	}
}

func TestBuildRequestBody_NonMarkdownNoCleanup(t *testing.T) {
	body, err := BuildRequestBody("screenshot", "http://example.com", nil, map[string]any{"url": "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	m := mustUnmarshalBody(t, body)
	if _, ok := m["addScriptTag"]; ok {
		t.Error("non-markdown endpoint should not have addScriptTag")
	}
}
