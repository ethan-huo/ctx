package markdown

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatSummary_Basic(t *testing.T) {
	doc := `# Introduction

Welcome to the project.
This is a great tool.

## Installation

Run npm install.
Then configure your settings.

## Usage

Use the CLI to run commands.
Pass flags for options.
`
	source := []byte(doc)
	headings := ParseHeadings(source)
	got := FormatSummary(source, headings, "https://example.com", "/tmp/cache/abc")

	// First line: summary header
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "[ctx:summary]") {
		t.Errorf("expected [ctx:summary] prefix, got: %q", lines[0])
	}
	if !strings.Contains(lines[0], "3 sections") {
		t.Errorf("expected 3 sections in header, got: %q", lines[0])
	}
	if !strings.Contains(lines[0], "ctx read https://example.com -s <number> (e.g. -s 10 or -s 10-20)") {
		t.Errorf("expected read command in header, got: %q", lines[0])
	}

	// Second line: cache path
	if !strings.Contains(lines[1], "Full content: /tmp/cache/abc") {
		t.Errorf("expected cache path line, got: %q", lines[1])
	}

	// Heading format: "# 1 Introduction (X lines)"
	if !strings.Contains(got, "# 1 Introduction") {
		t.Error("missing heading '# 1 Introduction'")
	}
	if !strings.Contains(got, "## 1.1 Installation") {
		t.Error("missing heading '## 1.1 Installation'")
	}
	if !strings.Contains(got, "## 1.2 Usage") {
		t.Error("missing heading '## 1.2 Usage'")
	}

	// Body preview should contain content with trailing ...
	if !strings.Contains(got, "Welcome to the project.") {
		t.Error("missing body preview for Introduction")
	}
	if !strings.Contains(got, "...") {
		t.Error("missing ... ellipsis in preview")
	}
}

func TestFormatSummary_EmptySections(t *testing.T) {
	doc := `# Title

## Empty Section

## Section With Content

Some actual content here.
`
	source := []byte(doc)
	headings := ParseHeadings(source)
	got := FormatSummary(source, headings, "https://example.com", "/tmp/cache")

	// The empty section heading should appear
	if !strings.Contains(got, "## 1.1 Empty Section") {
		t.Error("missing empty section heading")
	}

	// But the empty section should NOT have body preview lines between its heading
	// and the next heading. Find the empty section heading line, then check the next
	// non-blank line is the next heading.
	lines := strings.Split(got, "\n")
	var emptyIdx int
	for i, line := range lines {
		if strings.Contains(line, "## 1.1 Empty Section") {
			emptyIdx = i
			break
		}
	}
	// Next non-blank line after the empty section heading should be the next heading
	for j := emptyIdx + 1; j < len(lines); j++ {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "##") {
			t.Errorf("expected next heading after empty section, got: %q", trimmed)
		}
		break
	}
}

func TestFormatSummary_ProportionalPreview(t *testing.T) {
	// Build a large section (~60 lines of body) and a small section (~5 lines).
	var largeSectionLines []string
	largeSectionLines = append(largeSectionLines, "# Large Section", "")
	for i := 1; i <= 58; i++ {
		largeSectionLines = append(largeSectionLines, fmt.Sprintf("Line %d of the large section content.", i))
	}
	largeSectionLines = append(largeSectionLines, "", "# Small Section", "")
	for i := 1; i <= 3; i++ {
		largeSectionLines = append(largeSectionLines, fmt.Sprintf("Small line %d.", i))
	}
	largeSectionLines = append(largeSectionLines, "")

	doc := strings.Join(largeSectionLines, "\n")
	source := []byte(doc)
	headings := ParseHeadings(source)
	got := FormatSummary(source, headings, "https://example.com", "/tmp/cache")

	// Large section: sectionLines/10 > 5, so clamp to 5
	// Count preview lines for large section (between "# 1 Large Section" and "# 2 Small Section")
	lines := strings.Split(got, "\n")
	var largeStart, largeEnd int
	for i, line := range lines {
		if strings.Contains(line, "# 1 Large Section") {
			largeStart = i + 1
		}
		if strings.Contains(line, "# 2 Small Section") {
			largeEnd = i
			break
		}
	}
	var largePreviewCount int
	for _, line := range lines[largeStart:largeEnd] {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			largePreviewCount++
		}
	}
	if largePreviewCount != 5 {
		t.Errorf("large section: got %d preview lines, want 5", largePreviewCount)
	}

	// Small section: sectionLines ~5, sectionLines/10 = 0, clamp to 2
	var smallStart int
	for i, line := range lines {
		if strings.Contains(line, "# 2 Small Section") {
			smallStart = i + 1
			break
		}
	}
	var smallPreviewCount int
	for _, line := range lines[smallStart:] {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			smallPreviewCount++
		}
	}
	if smallPreviewCount != 2 {
		t.Errorf("small section: got %d preview lines, want 2", smallPreviewCount)
	}
}

func TestFormatLineSummary_Basic(t *testing.T) {
	// Build a document with 100 lines (no headings).
	var docLines []string
	for i := 1; i <= 100; i++ {
		docLines = append(docLines, fmt.Sprintf("data line %d", i))
	}
	doc := strings.Join(docLines, "\n")
	source := []byte(doc)
	got := FormatLineSummary(source, "/tmp/cache/xyz")

	// Header line
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "[ctx:summary] 100 lines, no sections.") {
		t.Errorf("unexpected header: %q", lines[0])
	}
	if !strings.Contains(lines[0], "/tmp/cache/xyz") {
		t.Errorf("missing cache path in header: %q", lines[0])
	}

	// Should have 5 windows
	windowCount := strings.Count(got, "Lines ")
	if windowCount != 5 {
		t.Errorf("got %d windows, want 5", windowCount)
	}

	// First window should start at line 1
	if !strings.Contains(got, "Lines 1-") {
		t.Error("first window should start at line 1")
	}

	// Each window should end with ...
	ellipsisCount := strings.Count(got, "...\n")
	if ellipsisCount != 5 {
		t.Errorf("got %d ellipsis markers, want 5", ellipsisCount)
	}
}

func TestFormatLineSummary_Short(t *testing.T) {
	doc := "line one\nline two\nline three\nline four\nline five"
	source := []byte(doc)
	got := FormatLineSummary(source, "/tmp/cache/short")

	// 5 lines is very short. Should produce fewer windows.
	if !strings.HasPrefix(got, "[ctx:summary] 5 lines, no sections.") {
		lines := strings.Split(got, "\n")
		t.Errorf("unexpected header: %q", lines[0])
	}

	// Should have at least 1 window
	windowCount := strings.Count(got, "Lines ")
	if windowCount < 1 {
		t.Error("expected at least 1 window")
	}

	// Should not have more than 5 windows
	if windowCount > 5 {
		t.Errorf("got %d windows, want at most 5", windowCount)
	}
}
