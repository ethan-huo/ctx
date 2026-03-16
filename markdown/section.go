package markdown

import (
	"fmt"
	"strings"
)

type Heading struct {
	Level    int
	Text     string
	Line     int // 1-based
	StartByte int
	EndByte   int
	Number   string // e.g. "1", "1.2", "3.1.1"
}

// ParseHeadings scans source for ATX headings, skipping fenced code blocks.
func ParseHeadings(source []byte) []Heading {
	var headings []Heading
	inFence := false
	offset := 0

	lines := strings.Split(string(source), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			offset += len(line) + 1
			continue
		}
		if inFence {
			offset += len(line) + 1
			continue
		}

		level, text := parseATXHeading(trimmed)
		if level > 0 {
			headings = append(headings, Heading{
				Level:     level,
				Text:      text,
				Line:      i + 1,
				StartByte: offset,
			})
		}
		offset += len(line) + 1
	}

	// Fill EndByte: each heading extends to the start of the next same-or-higher-level heading, or EOF.
	totalLen := len(source)
	for i := range headings {
		headings[i].EndByte = totalLen
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level <= headings[i].Level {
				headings[i].EndByte = headings[j].StartByte
				break
			}
		}
	}

	return NumberHeadings(headings)
}

func parseATXHeading(line string) (int, string) {
	if len(line) == 0 || line[0] != '#' {
		return 0, ""
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, ""
	}
	text := strings.TrimSpace(line[level+1:])
	// Strip optional closing sequence per CommonMark: trailing #+ optionally preceded by spaces
	// Only strip if the # sequence is preceded by a space (not mid-word like "C#")
	if end := len(text) - 1; end >= 0 && text[end] == '#' {
		i := end
		for i > 0 && text[i-1] == '#' {
			i--
		}
		if i == 0 {
			text = ""
		} else if text[i-1] == ' ' {
			text = strings.TrimRight(text[:i-1], " ")
		}
	}
	return level, text
}

// NumberHeadings assigns hierarchical numbers to headings.
func NumberHeadings(headings []Heading) []Heading {
	counters := make([]int, 7) // index 1-6

	for i := range headings {
		lvl := headings[i].Level
		counters[lvl]++
		// Reset all deeper counters
		for j := lvl + 1; j <= 6; j++ {
			counters[j] = 0
		}
		// Build number string from highest active level down to current
		var parts []string
		for j := 1; j <= lvl; j++ {
			if counters[j] > 0 {
				parts = append(parts, fmt.Sprintf("%d", counters[j]))
			}
		}
		headings[i].Number = strings.Join(parts, ".")
	}
	return headings
}

// ExtractSection returns the content for a heading (from StartByte to EndByte).
func ExtractSection(source []byte, h Heading) string {
	end := h.EndByte
	if end > len(source) {
		end = len(source)
	}
	return string(source[h.StartByte:end])
}

// FormatTOC produces a numbered outline with section line counts.
func FormatTOC(source []byte, headings []Heading) string {
	var b strings.Builder
	for _, h := range headings {
		end := h.EndByte
		if end > len(source) {
			end = len(source)
		}
		lines := strings.Count(string(source[h.StartByte:end]), "\n")
		indent := strings.Repeat("  ", h.Level-1)
		fmt.Fprintf(&b, "%s%-8s %s  (%d lines)\n", indent, h.Number, h.Text, lines)
	}
	return b.String()
}

// SectionRange represents a single or range section selector.
type SectionRange struct {
	From string
	To   string // empty for single section
}

// ParseSectionExpr parses a section expression like "1,2.3-5.1,6.2".
func ParseSectionExpr(expr string) ([]SectionRange, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty section expression")
	}

	segments := strings.Split(expr, ",")
	var ranges []SectionRange
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// Try to split on "-" but be careful: "1.2-3.1" should split into "1.2" and "3.1",
		// not at every "-". We split on the first "-" that is preceded by a digit.
		from, to, err := parseRangeSegment(seg)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, SectionRange{From: from, To: to})
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("no sections in expression: %q", expr)
	}
	return ranges, nil
}

func parseRangeSegment(seg string) (string, string, error) {
	// Find "-" that separates range endpoints.
	// Section numbers contain digits and dots only.
	// A range separator "-" appears between two section numbers: "1.2-3.1"
	// We need to find the "-" that is not part of a section number.
	// Section numbers are: [0-9]+(\.[0-9]+)*
	// So we look for "-" where left side is a digit and right side is a digit.
	for i := 1; i < len(seg)-1; i++ {
		if seg[i] == '-' && isDigit(seg[i-1]) && isDigit(seg[i+1]) {
			from := strings.TrimSpace(seg[:i])
			to := strings.TrimSpace(seg[i+1:])
			if !isValidNumber(from) {
				return "", "", fmt.Errorf("invalid section number: %q", from)
			}
			if !isValidNumber(to) {
				return "", "", fmt.Errorf("invalid section number: %q", to)
			}
			return from, to, nil
		}
	}
	// No range separator found — single section
	if !isValidNumber(seg) {
		return "", "", fmt.Errorf("invalid section number: %q", seg)
	}
	return seg, "", nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isValidNumber(s string) bool {
	if s == "" {
		return false
	}
	prevDot := true // treat start as "after dot" to reject leading dot
	for _, c := range s {
		if c == '.' {
			if prevDot {
				return false // leading dot or consecutive dots
			}
			prevDot = true
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
		prevDot = false
	}
	return !prevDot // reject trailing dot
}

// ExpandRanges resolves SectionRanges against a heading list, returning
// deduplicated headings in TOC order.
func ExpandRanges(headings []Heading, ranges []SectionRange) ([]Heading, error) {
	// Build index: number -> position
	idx := make(map[string]int, len(headings))
	for i, h := range headings {
		idx[h.Number] = i
	}

	seen := make(map[int]bool)
	var result []Heading

	for _, r := range ranges {
		if r.To == "" {
			// Single section
			pos, ok := idx[r.From]
			if !ok {
				return nil, fmt.Errorf("section %q not found", r.From)
			}
			if !seen[pos] {
				seen[pos] = true
				result = append(result, headings[pos])
			}
		} else {
			// Range
			fromPos, ok := idx[r.From]
			if !ok {
				return nil, fmt.Errorf("section %q not found", r.From)
			}
			toPos, ok := idx[r.To]
			if !ok {
				return nil, fmt.Errorf("section %q not found", r.To)
			}
			if fromPos > toPos {
				fromPos, toPos = toPos, fromPos
			}
			for i := fromPos; i <= toPos; i++ {
				if !seen[i] {
					seen[i] = true
					result = append(result, headings[i])
				}
			}
		}
	}

	// Sort by original TOC order
	sortByIndex(result, idx)
	return result, nil
}

func sortByIndex(hs []Heading, idx map[string]int) {
	// Simple insertion sort — heading lists are small
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && idx[hs[j].Number] < idx[hs[j-1].Number]; j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
}
