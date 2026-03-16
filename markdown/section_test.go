package markdown

import (
	"strings"
	"testing"
)

// --- Test Document ---

const testDoc = `# Getting Started

Some intro text.

## Installation

Install instructions here.

## Quick Start

Quick start guide.

### Hello World

Hello world example.

# Configuration

Config details.

# API Reference

API intro.

## Authentication

Auth docs.

` + "```" + `
# This is not a heading
` + "```" + `

## Endpoints

Endpoint docs.
`

// ===== ParseHeadings =====

func TestParseHeadings(t *testing.T) {
	headings := ParseHeadings([]byte(testDoc))

	want := []struct {
		number string
		text   string
	}{
		{"1", "Getting Started"},
		{"1.1", "Installation"},
		{"1.2", "Quick Start"},
		{"1.2.1", "Hello World"},
		{"2", "Configuration"},
		{"3", "API Reference"},
		{"3.1", "Authentication"},
		{"3.2", "Endpoints"},
	}

	if len(headings) != len(want) {
		t.Fatalf("got %d headings, want %d", len(headings), len(want))
	}
	for i, w := range want {
		if headings[i].Number != w.number {
			t.Errorf("heading[%d].Number = %q, want %q", i, headings[i].Number, w.number)
		}
		if headings[i].Text != w.text {
			t.Errorf("heading[%d].Text = %q, want %q", i, headings[i].Text, w.text)
		}
	}
}

func TestParseHeadings_EmptyDocument(t *testing.T) {
	headings := ParseHeadings([]byte(""))
	if len(headings) != 0 {
		t.Errorf("got %d headings for empty doc, want 0", len(headings))
	}
}

func TestParseHeadings_NoHeadings(t *testing.T) {
	headings := ParseHeadings([]byte("Just plain text.\nNo headings here.\n"))
	if len(headings) != 0 {
		t.Errorf("got %d headings, want 0", len(headings))
	}
}

func TestParseHeadings_SkipsFencedCode(t *testing.T) {
	headings := ParseHeadings([]byte(testDoc))
	for _, h := range headings {
		if h.Text == "This is not a heading" {
			t.Error("fenced code block heading was not skipped")
		}
	}
}

func TestParseHeadings_TildeFence(t *testing.T) {
	doc := "# Real\n\n~~~\n# Fake\n~~~\n\n## Also Real\n"
	headings := ParseHeadings([]byte(doc))
	if len(headings) != 2 {
		t.Fatalf("got %d headings, want 2", len(headings))
	}
	if headings[0].Text != "Real" || headings[1].Text != "Also Real" {
		t.Errorf("got %q / %q", headings[0].Text, headings[1].Text)
	}
}

func TestParseHeadings_H7NotHeading(t *testing.T) {
	doc := "####### Not a heading\n# Real\n"
	headings := ParseHeadings([]byte(doc))
	if len(headings) != 1 {
		t.Fatalf("got %d headings, want 1", len(headings))
	}
	if headings[0].Text != "Real" {
		t.Errorf("got %q, want %q", headings[0].Text, "Real")
	}
}

func TestParseHeadings_NoSpaceAfterHash(t *testing.T) {
	doc := "#NoSpace\n# With Space\n"
	headings := ParseHeadings([]byte(doc))
	if len(headings) != 1 {
		t.Fatalf("got %d headings, want 1", len(headings))
	}
	if headings[0].Text != "With Space" {
		t.Errorf("got %q", headings[0].Text)
	}
}

func TestParseHeadings_TrailingHashes(t *testing.T) {
	doc := "# Title ##\n## Sub ###\n"
	headings := ParseHeadings([]byte(doc))
	if len(headings) != 2 {
		t.Fatalf("got %d headings, want 2", len(headings))
	}
	if headings[0].Text != "Title" {
		t.Errorf("heading[0].Text = %q, want %q", headings[0].Text, "Title")
	}
	if headings[1].Text != "Sub" {
		t.Errorf("heading[1].Text = %q, want %q", headings[1].Text, "Sub")
	}
}

func TestParseHeadings_HashInText(t *testing.T) {
	doc := "# C# Language\n## F# Basics\n"
	headings := ParseHeadings([]byte(doc))
	if len(headings) != 2 {
		t.Fatalf("got %d headings, want 2", len(headings))
	}
	if headings[0].Text != "C# Language" {
		t.Errorf("heading[0].Text = %q, want %q", headings[0].Text, "C# Language")
	}
	if headings[1].Text != "F# Basics" {
		t.Errorf("heading[1].Text = %q, want %q", headings[1].Text, "F# Basics")
	}
}

func TestParseHeadings_DeepNesting(t *testing.T) {
	doc := "# A\n## B\n### C\n#### D\n##### E\n###### F\n"
	headings := ParseHeadings([]byte(doc))
	want := []string{"1", "1.1", "1.1.1", "1.1.1.1", "1.1.1.1.1", "1.1.1.1.1.1"}
	if len(headings) != len(want) {
		t.Fatalf("got %d headings, want %d", len(headings), len(want))
	}
	for i, w := range want {
		if headings[i].Number != w {
			t.Errorf("heading[%d].Number = %q, want %q", i, headings[i].Number, w)
		}
	}
}

func TestParseHeadings_LineNumbers(t *testing.T) {
	doc := "# First\n\ntext\n\n## Second\n"
	headings := ParseHeadings([]byte(doc))
	if headings[0].Line != 1 {
		t.Errorf("first heading line = %d, want 1", headings[0].Line)
	}
	if headings[1].Line != 5 {
		t.Errorf("second heading line = %d, want 5", headings[1].Line)
	}
}

func TestParseHeadings_NumberResetOnSameLevel(t *testing.T) {
	doc := "# A\n## A1\n## A2\n# B\n## B1\n"
	headings := ParseHeadings([]byte(doc))
	want := []string{"1", "1.1", "1.2", "2", "2.1"}
	for i, w := range want {
		if headings[i].Number != w {
			t.Errorf("heading[%d].Number = %q, want %q", i, headings[i].Number, w)
		}
	}
}

// ===== ExtractSection =====

func TestExtractSection_SubsectionBoundary(t *testing.T) {
	source := []byte(testDoc)
	headings := ParseHeadings(source)

	// Section 1.1 (Installation) should NOT include Quick Start content
	section := extractByNumber(source, headings, "1.1")
	if !strings.Contains(section, "Install instructions here.") {
		t.Error("missing expected content")
	}
	if strings.Contains(section, "Quick start guide.") {
		t.Error("leaked next sibling content")
	}
}

func TestExtractSection_ParentIncludesChildren(t *testing.T) {
	source := []byte(testDoc)
	headings := ParseHeadings(source)

	// Section 1 (Getting Started) should include its children up to the next h1
	section := extractByNumber(source, headings, "1")
	if !strings.Contains(section, "Install instructions here.") {
		t.Error("parent section missing child content (Installation)")
	}
	if !strings.Contains(section, "Hello world example.") {
		t.Error("parent section missing grandchild content (Hello World)")
	}
	if strings.Contains(section, "Config details.") {
		t.Error("parent section leaked into next h1")
	}
}

func TestExtractSection_LastSection(t *testing.T) {
	source := []byte(testDoc)
	headings := ParseHeadings(source)

	section := extractByNumber(source, headings, "3.2")
	if !strings.Contains(section, "Endpoint docs.") {
		t.Error("last section missing content")
	}
}

func TestExtractSection_FirstSection(t *testing.T) {
	source := []byte(testDoc)
	headings := ParseHeadings(source)

	section := extractByNumber(source, headings, "1")
	if !strings.HasPrefix(section, "# Getting Started") {
		t.Errorf("first section should start with heading, got: %q", section[:40])
	}
}

func extractByNumber(source []byte, headings []Heading, number string) string {
	for _, h := range headings {
		if h.Number == number {
			return ExtractSection(source, h)
		}
	}
	return ""
}

// ===== FormatTOC =====

func TestFormatTOC(t *testing.T) {
	src := []byte(testDoc)
	headings := ParseHeadings(src)
	toc := FormatTOC(src, headings)

	if !strings.Contains(toc, "1.2.1") {
		t.Error("TOC missing nested heading number")
	}
	lines := strings.Split(strings.TrimSpace(toc), "\n")
	if len(lines) != 8 {
		t.Errorf("TOC has %d lines, want 8", len(lines))
	}
}

func TestFormatTOC_Empty(t *testing.T) {
	toc := FormatTOC(nil, nil)
	if toc != "" {
		t.Errorf("FormatTOC(nil) = %q, want empty", toc)
	}
}

func TestFormatTOC_ContainsLineCounts(t *testing.T) {
	src := []byte(testDoc)
	headings := ParseHeadings(src)
	toc := FormatTOC(src, headings)
	if !strings.Contains(toc, "lines)") {
		t.Error("TOC missing line count annotation")
	}
}

// ===== ParseSectionExpr =====

func TestParseSectionExpr_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  []SectionRange
	}{
		// Single
		{"1", []SectionRange{{From: "1"}}},
		{"3.1", []SectionRange{{From: "3.1"}}},
		{"1.2.3", []SectionRange{{From: "1.2.3"}}},

		// Multiple singles
		{"1,3.1,6.2", []SectionRange{{From: "1"}, {From: "3.1"}, {From: "6.2"}}},

		// Range
		{"1-3", []SectionRange{{From: "1", To: "3"}}},
		{"1.2-3.1", []SectionRange{{From: "1.2", To: "3.1"}}},

		// Mixed
		{"1-2,3.2-5.1,6.2", []SectionRange{
			{From: "1", To: "2"},
			{From: "3.2", To: "5.1"},
			{From: "6.2"},
		}},

		// Whitespace tolerance
		{" 1 , 2 ", []SectionRange{{From: "1"}, {From: "2"}}},

		// Trailing comma (empty segment skipped)
		{"1,2,", []SectionRange{{From: "1"}, {From: "2"}}},
	}

	for _, tt := range tests {
		ranges, err := ParseSectionExpr(tt.input)
		if err != nil {
			t.Errorf("ParseSectionExpr(%q) error: %v", tt.input, err)
			continue
		}
		if len(ranges) != len(tt.want) {
			t.Errorf("ParseSectionExpr(%q) = %d ranges, want %d", tt.input, len(ranges), len(tt.want))
			continue
		}
		for i, r := range ranges {
			if r.From != tt.want[i].From || r.To != tt.want[i].To {
				t.Errorf("ParseSectionExpr(%q)[%d] = {%q, %q}, want {%q, %q}",
					tt.input, i, r.From, r.To, tt.want[i].From, tt.want[i].To)
			}
		}
	}
}

func TestParseSectionExpr_Invalid(t *testing.T) {
	tests := []struct {
		input   string
		wantErr string
	}{
		{"", "empty section expression"},
		{"abc", "invalid section number"},
		{".1", "invalid section number"},
		{"1.", "invalid section number"},
		{"1..2", "invalid section number"},
		{"1,abc", "invalid section number"},
		{"1-", "invalid section number"},       // trailing dash, treated as single, fails validation
		{"-1", "invalid section number"},       // leading dash
		{"1.2.3.abc", "invalid section number"},
	}

	for _, tt := range tests {
		_, err := ParseSectionExpr(tt.input)
		if err == nil {
			t.Errorf("ParseSectionExpr(%q) = nil error, want error containing %q", tt.input, tt.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("ParseSectionExpr(%q) error = %q, want containing %q", tt.input, err.Error(), tt.wantErr)
		}
	}
}

// ===== ExpandRanges =====

func testHeadings() []Heading {
	return ParseHeadings([]byte(testDoc))
}

func headingNumbers(hs []Heading) []string {
	nums := make([]string, len(hs))
	for i, h := range hs {
		nums[i] = h.Number
	}
	return nums
}

func TestExpandRanges_SingleSection(t *testing.T) {
	hs := testHeadings()
	result, err := ExpandRanges(hs, []SectionRange{{From: "1.1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1.1"}
	assertNumbers(t, "single", got, want)
}

func TestExpandRanges_BasicRange(t *testing.T) {
	hs := testHeadings()
	result, err := ExpandRanges(hs, []SectionRange{{From: "1", To: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1", "1.1", "1.2", "1.2.1", "2"}
	assertNumbers(t, "1-2", got, want)
}

func TestExpandRanges_RangeAcrossLevels(t *testing.T) {
	hs := testHeadings()
	// 1.2 to 3.1: should include 1.2, 1.2.1, 2, 3, 3.1
	result, err := ExpandRanges(hs, []SectionRange{{From: "1.2", To: "3.1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1.2", "1.2.1", "2", "3", "3.1"}
	assertNumbers(t, "1.2-3.1", got, want)
}

func TestExpandRanges_OverlappingRanges(t *testing.T) {
	hs := testHeadings()
	// Two overlapping ranges: 1-2 and 1.2-3 should deduplicate
	result, err := ExpandRanges(hs, []SectionRange{
		{From: "1", To: "2"},
		{From: "1.2", To: "3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1", "1.1", "1.2", "1.2.1", "2", "3"}
	assertNumbers(t, "overlapping", got, want)
}

func TestExpandRanges_DuplicateSelections(t *testing.T) {
	hs := testHeadings()
	// Same section referenced multiple times
	result, err := ExpandRanges(hs, []SectionRange{
		{From: "1.1"},
		{From: "1.1"},
		{From: "1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1.1"}
	assertNumbers(t, "duplicates", got, want)
}

func TestExpandRanges_SingleAndRangeOverlap(t *testing.T) {
	hs := testHeadings()
	// Single "1.2" is already within range "1-2"
	result, err := ExpandRanges(hs, []SectionRange{
		{From: "1.2"},
		{From: "1", To: "2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1", "1.1", "1.2", "1.2.1", "2"}
	assertNumbers(t, "single+range overlap", got, want)
}

func TestExpandRanges_ReversedRange(t *testing.T) {
	hs := testHeadings()
	// "3-1" reversed — should auto-swap and work
	result, err := ExpandRanges(hs, []SectionRange{{From: "3", To: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1", "1.1", "1.2", "1.2.1", "2", "3"}
	assertNumbers(t, "reversed", got, want)
}

func TestExpandRanges_SameStartEnd(t *testing.T) {
	hs := testHeadings()
	// Range where start == end is just a single section
	result, err := ExpandRanges(hs, []SectionRange{{From: "2", To: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"2"}
	assertNumbers(t, "same start/end", got, want)
}

func TestExpandRanges_FullRange(t *testing.T) {
	hs := testHeadings()
	result, err := ExpandRanges(hs, []SectionRange{{From: "1", To: "3.2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != len(hs) {
		t.Errorf("full range: got %d sections, want %d", len(result), len(hs))
	}
}

func TestExpandRanges_MultipleDisjointRanges(t *testing.T) {
	hs := testHeadings()
	// 1.1 and 3.1-3.2 — no overlap, should return in TOC order
	result, err := ExpandRanges(hs, []SectionRange{
		{From: "3.1", To: "3.2"},
		{From: "1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1.1", "3.1", "3.2"}
	assertNumbers(t, "disjoint", got, want)
}

func TestExpandRanges_TOCOrderPreserved(t *testing.T) {
	hs := testHeadings()
	// Provide sections in reverse order — output should still be in TOC order
	result, err := ExpandRanges(hs, []SectionRange{
		{From: "3.2"},
		{From: "2"},
		{From: "1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := headingNumbers(result)
	want := []string{"1.1", "2", "3.2"}
	assertNumbers(t, "toc order", got, want)
}

func TestExpandRanges_NotFound(t *testing.T) {
	hs := testHeadings()

	_, err := ExpandRanges(hs, []SectionRange{{From: "99"}})
	if err == nil {
		t.Error("expected error for non-existent section")
	}

	_, err = ExpandRanges(hs, []SectionRange{{From: "1", To: "99"}})
	if err == nil {
		t.Error("expected error for non-existent range endpoint")
	}

	_, err = ExpandRanges(hs, []SectionRange{{From: "99", To: "1"}})
	if err == nil {
		t.Error("expected error for non-existent range start")
	}
}

func TestExpandRanges_EmptyInput(t *testing.T) {
	hs := testHeadings()
	result, err := ExpandRanges(hs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("got %d results for nil ranges, want 0", len(result))
	}
}

// ===== isValidNumber =====

func TestIsValidNumber(t *testing.T) {
	valid := []string{"1", "12", "1.2", "1.2.3", "10.20.30"}
	for _, s := range valid {
		if !isValidNumber(s) {
			t.Errorf("isValidNumber(%q) = false, want true", s)
		}
	}

	invalid := []string{"", ".", ".1", "1.", "1..2", "abc", "1.a", "1.2.", ".1.2"}
	for _, s := range invalid {
		if isValidNumber(s) {
			t.Errorf("isValidNumber(%q) = true, want false", s)
		}
	}
}

// ===== Helpers =====

func assertNumbers(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: [%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
