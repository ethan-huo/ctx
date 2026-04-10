package markdown

import (
	"fmt"
	"strings"
)

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// FormatSummary generates a structural summary for long documents WITH headings.
// It shows all headings with line counts and proportional content previews.
func FormatSummary(source []byte, headings []Heading, url, cachePath string) string {
	totalLines := strings.Count(string(source), "\n") + 1

	var b strings.Builder
	fmt.Fprintf(&b, "[ctx:summary] %d lines, %d sections. Read sections: ctx read %s -s <number> (e.g. -s 10 or -s 10-20)\n", totalLines, len(headings), url)
	fmt.Fprintf(&b, "Full content: %s\n", cachePath)

	for _, h := range headings {
		end := h.EndByte
		if end > len(source) {
			end = len(source)
		}
		sectionText := string(source[h.StartByte:end])
		sectionLines := strings.Count(sectionText, "\n")

		hashes := strings.Repeat("#", h.Level)
		fmt.Fprintf(&b, "\n%s %s %s (%d lines)\n", hashes, h.Number, h.Text, sectionLines)

		lines := strings.Split(sectionText, "\n")
		// Skip the first line (the heading itself), collect non-empty body lines.
		var body []string
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				body = append(body, trimmed)
			}
		}

		n := clamp(sectionLines/10, 2, 5)
		if len(body) == 0 {
			continue
		}
		if n > len(body) {
			n = len(body)
		}
		for i := 0; i < n; i++ {
			b.WriteString(body[i])
			if i == n-1 {
				b.WriteString("...")
			}
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// FormatLineSummary generates a line-window summary for long documents WITHOUT headings.
// Samples up to 5 evenly-spaced windows across the document.
func FormatLineSummary(source []byte, cachePath string) string {
	allLines := strings.Split(string(source), "\n")
	totalLines := len(allLines)

	var b strings.Builder
	fmt.Fprintf(&b, "[ctx:summary] %d lines, no sections. Full content: %s\n", totalLines, cachePath)

	windowSize := clamp(totalLines/20, 3, 5)

	numWindows := 5
	if totalLines < numWindows*windowSize {
		numWindows = totalLines / windowSize
		if numWindows < 1 {
			numWindows = 1
		}
	}

	for i := 0; i < numWindows; i++ {
		var start int
		if numWindows == 1 {
			start = 0
		} else {
			// Evenly space windows so first starts at 0, last includes final lines.
			maxStart := totalLines - windowSize
			if maxStart < 0 {
				maxStart = 0
			}
			start = i * maxStart / (numWindows - 1)
		}

		end := start + windowSize
		if end > totalLines {
			end = totalLines
		}

		// Display uses 1-based line numbers.
		fmt.Fprintf(&b, "\nLines %d-%d:\n", start+1, end)
		for _, line := range allLines[start:end] {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString("...\n")
	}

	return b.String()
}
