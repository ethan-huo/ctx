package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

const youTubeSectionWindowSeconds = 15 * 60
const youTubeMetadataTimeout = 90 * time.Second

var youtubeMetadataLoader = loadYouTubeMetadata
var youtubeSubtitleLoader = loadYouTubeSubtitle

type youTubeMetadata struct {
	VideoID       string
	CanonicalURL  string
	Title         string
	Channel       string
	Duration      time.Duration
	CaptionKind   string
	CaptionLang   string
	CaptionName   string
	CaptionURL    string
	CaptionTracks []youTubeTrackSelection
	Chapters      []youTubeChapter
	TranscriptURL string
}

type youTubeChapter struct {
	Title string
	Start time.Duration
	End   time.Duration
}

type youTubeCue struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

type youTubeTranscriptResponse struct {
	Events []struct {
		StartMs    int `json:"tStartMs"`
		DurationMs int `json:"dDurationMs"`
		Segments   []struct {
			Text string `json:"utf8"`
		} `json:"segs"`
	} `json:"events"`
}

type youTubeMetadataResponse struct {
	ID                string                         `json:"id"`
	Title             string                         `json:"title"`
	Channel           string                         `json:"channel"`
	Uploader          string                         `json:"uploader"`
	DurationSeconds   float64                        `json:"duration"`
	Chapters          []youTubeMetadataChapter       `json:"chapters"`
	Subtitles         map[string][]youTubeTrackEntry `json:"subtitles"`
	AutomaticCaptions map[string][]youTubeTrackEntry `json:"automatic_captions"`
}

type youTubeMetadataChapter struct {
	Title        string  `json:"title"`
	StartSeconds float64 `json:"start_time"`
	EndSeconds   float64 `json:"end_time"`
}

type youTubeTrackEntry struct {
	Ext  string `json:"ext"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type youTubeTrackSelection struct {
	Kind     string
	Lang     string
	Name     string
	URL      string
	Priority int
}

var youTubeHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

func fetchYouTubeTranscript(rawURL string) (string, error) {
	meta, err := youtubeMetadataLoader(rawURL)
	if err != nil {
		return "", err
	}

	selection, cues, ok, err := resolveYouTubeTrack(meta)
	if err != nil {
		return "", err
	}
	if !ok {
		return renderYouTubeUnavailable(meta), nil
	}
	applyYouTubeTrack(&meta, selection)

	return renderYouTubeTranscript(meta, cues), nil
}

func resolveYouTubeTrack(meta youTubeMetadata) (youTubeTrackSelection, []youTubeCue, bool, error) {
	candidates := meta.CaptionTracks
	if len(candidates) == 0 && meta.CaptionURL != "" {
		candidates = []youTubeTrackSelection{{
			Kind: meta.CaptionKind,
			Lang: meta.CaptionLang,
			Name: meta.CaptionName,
			URL:  meta.CaptionURL,
		}}
	}
	if len(candidates) == 0 {
		return youTubeTrackSelection{}, nil, false, nil
	}

	var fallbackTrack youTubeTrackSelection
	var fallbackCues []youTubeCue
	for _, candidate := range candidates {
		cues, err := youtubeSubtitleLoader(candidate.URL)
		if err != nil {
			return youTubeTrackSelection{}, nil, false, err
		}
		if len(cues) == 0 {
			continue
		}
		if fallbackTrack.URL == "" {
			fallbackTrack = candidate
			fallbackCues = cues
		}
		if looksLikeTranslatedYouTubeTrack(cues) {
			continue
		}
		return candidate, cues, true, nil
	}
	if fallbackTrack.URL == "" {
		return youTubeTrackSelection{}, nil, false, nil
	}
	return fallbackTrack, fallbackCues, true, nil
}

func loadYouTubeMetadata(rawURL string) (youTubeMetadata, error) {
	videoID, canonicalURL, ok := parseYouTubeURL(rawURL)
	if !ok {
		return youTubeMetadata{}, fmt.Errorf("invalid YouTube URL: %s", rawURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), youTubeMetadataTimeout)
	defer cancel()

	args := []string{
		"-J",
		"--skip-download",
		"--no-playlist",
		"--no-warnings",
		"--write-subs",
		"--write-auto-subs",
		canonicalURL,
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return youTubeMetadata{}, fmt.Errorf("timed out while resolving YouTube metadata for %s", canonicalURL)
		}
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return youTubeMetadata{}, fmt.Errorf("YouTube transcript reads require yt-dlp on PATH. Install yt-dlp and retry.")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(err.Error())
		}
		return youTubeMetadata{}, fmt.Errorf("yt-dlp failed for %s: %s", canonicalURL, msg)
	}

	meta, err := parseYouTubeMetadata(stdout, canonicalURL)
	if err != nil {
		return youTubeMetadata{}, err
	}
	meta.VideoID = videoID
	return meta, nil
}

func parseYouTubeMetadata(data []byte, canonicalURL string) (youTubeMetadata, error) {
	var resp youTubeMetadataResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return youTubeMetadata{}, fmt.Errorf("failed to parse yt-dlp metadata: %w", err)
	}

	meta := youTubeMetadata{
		VideoID:      resp.ID,
		CanonicalURL: canonicalURL,
		Title:        strings.TrimSpace(resp.Title),
		Channel:      firstNonEmpty(strings.TrimSpace(resp.Channel), strings.TrimSpace(resp.Uploader)),
		Duration:     secondsToDuration(resp.DurationSeconds),
	}
	meta.Chapters = normalizeYouTubeChapters(resp.Chapters, meta.Duration)

	if tracks := chooseYouTubeTracks(resp.Subtitles, "subtitles"); len(tracks) > 0 {
		meta.CaptionTracks = tracks
		applyYouTubeTrack(&meta, tracks[0])
		return meta, nil
	}
	if tracks := chooseYouTubeTracks(resp.AutomaticCaptions, "automatic captions"); len(tracks) > 0 {
		meta.CaptionTracks = tracks
		applyYouTubeTrack(&meta, tracks[0])
	}

	return meta, nil
}

func applyYouTubeTrack(meta *youTubeMetadata, track youTubeTrackSelection) {
	meta.CaptionKind = track.Kind
	meta.CaptionLang = track.Lang
	meta.CaptionName = track.Name
	meta.CaptionURL = track.URL
}

func chooseYouTubeTracks(tracks map[string][]youTubeTrackEntry, kind string) []youTubeTrackSelection {
	if len(tracks) == 0 {
		return nil
	}

	var exact []youTubeTrackSelection
	var translated []youTubeTrackSelection
	for lang, entries := range tracks {
		entry, ok := chooseYouTubeTrackEntry(entries)
		if !ok {
			continue
		}
		selection := youTubeTrackSelection{
			Kind:     kind,
			Lang:     lang,
			Name:     entry.Name,
			URL:      entry.URL,
			Priority: youTubeLanguagePriority(lang),
		}
		if isPreferredOriginalYouTubeLang(lang) {
			exact = append(exact, selection)
			continue
		}
		translated = append(translated, selection)
	}

	for _, candidates := range [][]youTubeTrackSelection{exact, translated} {
		if len(candidates) == 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Priority != candidates[j].Priority {
				return candidates[i].Priority < candidates[j].Priority
			}
			return candidates[i].Lang < candidates[j].Lang
		})
		return candidates
	}

	return nil
}

func chooseYouTubeTrackEntry(entries []youTubeTrackEntry) (youTubeTrackEntry, bool) {
	if len(entries) == 0 {
		return youTubeTrackEntry{}, false
	}
	for _, entry := range entries {
		if entry.Ext == "json3" && entry.URL != "" {
			return entry, true
		}
	}
	for _, entry := range entries {
		if entry.URL != "" {
			return entry, true
		}
	}
	return youTubeTrackEntry{}, false
}

func isPreferredOriginalYouTubeLang(lang string) bool {
	if strings.HasSuffix(lang, "-orig") {
		return true
	}
	return !strings.Contains(lang, "-")
}

func youTubeLanguagePriority(lang string) int {
	base := strings.TrimSuffix(lang, "-orig")
	switch base {
	case "en":
		return 0
	case "zh", "zh-Hans", "zh-Hant":
		return 1
	case "ja":
		return 2
	default:
		if !strings.Contains(base, "-") {
			return 10
		}
		return 20
	}
}

func loadYouTubeSubtitle(subtitleURL string) ([]youTubeCue, error) {
	req, err := http.NewRequest("GET", subtitleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("YouTube subtitle request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var transcript youTubeTranscriptResponse
	if err := json.NewDecoder(resp.Body).Decode(&transcript); err != nil {
		return nil, fmt.Errorf("failed to parse YouTube transcript JSON: %w", err)
	}

	return parseYouTubeCues(transcript.Events), nil
}

func parseYouTubeCues(events []struct {
	StartMs    int `json:"tStartMs"`
	DurationMs int `json:"dDurationMs"`
	Segments   []struct {
		Text string `json:"utf8"`
	} `json:"segs"`
}) []youTubeCue {
	cues := make([]youTubeCue, 0, len(events))
	for _, event := range events {
		text := cleanYouTubeTranscriptText(event.Segments)
		if text == "" {
			continue
		}
		start := time.Duration(event.StartMs) * time.Millisecond
		end := start + time.Duration(event.DurationMs)*time.Millisecond
		if end < start {
			end = start
		}
		cues = append(cues, youTubeCue{
			Start: start,
			End:   end,
			Text:  text,
		})
	}
	return cues
}

func cleanYouTubeTranscriptText(segments []struct {
	Text string `json:"utf8"`
}) string {
	var parts []string
	for _, segment := range segments {
		if strings.TrimSpace(segment.Text) == "" {
			continue
		}
		text := html.UnescapeString(segment.Text)
		text = youTubeHTMLTagPattern.ReplaceAllString(text, "")
		text = strings.Join(strings.Fields(text), " ")
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func looksLikeTranslatedYouTubeTrack(cues []youTubeCue) bool {
	if len(cues) == 0 {
		return false
	}

	limit := min(len(cues), 3)
	var sample []string
	for i := 0; i < limit; i++ {
		text := strings.TrimSpace(cues[i].Text)
		if text != "" {
			sample = append(sample, strings.ToLower(text))
		}
	}
	joined := strings.Join(sample, " ")
	markers := []string{
		"translated by ai",
		"cannot guarantee its accuracy",
		"字幕由 ai 翻译",
		"无法保证其准确性",
	}
	for _, marker := range markers {
		if strings.Contains(joined, marker) {
			return true
		}
	}
	return false
}

func renderYouTubeTranscript(meta youTubeMetadata, cues []youTubeCue) string {
	var b strings.Builder
	b.WriteString("Title: ")
	b.WriteString(firstNonEmpty(meta.Title, "YouTube Video"))
	b.WriteString("\n\n")
	writeYouTubeMetadata(&b, meta)

	sections := buildYouTubeSections(meta, cues)
	for _, section := range sections {
		b.WriteString("\n# ")
		b.WriteString(sectionHeading(section))
		b.WriteString("\n\n")
		for _, cue := range section.Cues {
			b.WriteString("[")
			b.WriteString(formatDurationClock(cue.Start))
			b.WriteString("] ")
			b.WriteString(cue.Text)
			b.WriteByte('\n')
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderYouTubeUnavailable(meta youTubeMetadata) string {
	var b strings.Builder
	b.WriteString("Title: ")
	b.WriteString(firstNonEmpty(meta.Title, "YouTube Video"))
	b.WriteString("\n\n")
	writeYouTubeMetadata(&b, meta)
	b.WriteString("\nNo subtitles or captions are available for this video.\n")
	return strings.TrimRight(b.String(), "\n")
}

func writeYouTubeMetadata(b *strings.Builder, meta youTubeMetadata) {
	fmt.Fprintf(b, "- source: YouTube\n")
	if meta.Channel != "" {
		fmt.Fprintf(b, "- channel: %s\n", meta.Channel)
	}
	if meta.Duration > 0 {
		fmt.Fprintf(b, "- duration: %s\n", formatDurationClock(meta.Duration))
	}
	if meta.CaptionKind != "" {
		fmt.Fprintf(b, "- captions: %s\n", meta.CaptionKind)
	}
	if meta.CaptionLang != "" {
		fmt.Fprintf(b, "- language: %s\n", meta.CaptionLang)
	}
	if meta.CanonicalURL != "" {
		fmt.Fprintf(b, "- url: %s\n", meta.CanonicalURL)
	}
}

type youTubeSection struct {
	Title string
	Start time.Duration
	End   time.Duration
	Cues  []youTubeCue
}

func buildYouTubeSections(meta youTubeMetadata, cues []youTubeCue) []youTubeSection {
	if len(cues) == 0 {
		return nil
	}

	chapters := meta.Chapters
	if len(chapters) > 0 {
		return buildYouTubeChapterSections(chapters, cues)
	}
	return buildYouTubeWindowSections(meta.Duration, cues)
}

func buildYouTubeChapterSections(chapters []youTubeChapter, cues []youTubeCue) []youTubeSection {
	sections := make([]youTubeSection, 0, len(chapters))
	cueIndex := 0
	for _, chapter := range chapters {
		section := youTubeSection{
			Title: chapter.Title,
			Start: chapter.Start,
			End:   chapter.End,
		}
		for cueIndex < len(cues) && cues[cueIndex].Start < chapter.Start {
			cueIndex++
		}
		for j := cueIndex; j < len(cues); j++ {
			if chapter.End > 0 && cues[j].Start >= chapter.End {
				break
			}
			section.Cues = append(section.Cues, cues[j])
			cueIndex = j + 1
		}
		if len(section.Cues) > 0 {
			sections = append(sections, section)
		}
	}
	if len(sections) > 0 {
		return sections
	}
	return buildYouTubeWindowSections(0, cues)
}

func buildYouTubeWindowSections(duration time.Duration, cues []youTubeCue) []youTubeSection {
	total := duration
	if total <= 0 {
		total = cues[len(cues)-1].End
	}
	if total <= 0 {
		total = cues[len(cues)-1].Start
	}
	if total <= 0 {
		total = time.Duration(youTubeSectionWindowSeconds) * time.Second
	}

	window := time.Duration(youTubeSectionWindowSeconds) * time.Second
	sections := make([]youTubeSection, 0, int(total/window)+1)
	cueIndex := 0
	for start := time.Duration(0); start < total; start += window {
		end := start + window
		if end > total {
			end = total
		}
		section := youTubeSection{
			Start: start,
			End:   end,
		}
		for cueIndex < len(cues) && cues[cueIndex].Start < start {
			cueIndex++
		}
		for j := cueIndex; j < len(cues); j++ {
			if cues[j].Start >= end {
				break
			}
			section.Cues = append(section.Cues, cues[j])
			cueIndex = j + 1
		}
		if len(section.Cues) > 0 {
			sections = append(sections, section)
		}
	}
	return sections
}

func sectionHeading(section youTubeSection) string {
	timerange := formatDurationClock(section.Start) + "-" + formatDurationClock(section.End)
	title := normalizeYouTubeChapterTitle(section.Title)
	if title == "" {
		return timerange
	}
	return timerange + " — " + title
}

func normalizeYouTubeChapterTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if strings.HasPrefix(title, "<Untitled Chapter") && strings.HasSuffix(title, ">") {
		return ""
	}
	return title
}

func normalizeYouTubeChapters(chapters []youTubeMetadataChapter, duration time.Duration) []youTubeChapter {
	if len(chapters) == 0 {
		return nil
	}

	result := make([]youTubeChapter, 0, len(chapters))
	for i, chapter := range chapters {
		start := secondsToDuration(chapter.StartSeconds)
		end := secondsToDuration(chapter.EndSeconds)
		if end <= start {
			if i+1 < len(chapters) {
				end = secondsToDuration(chapters[i+1].StartSeconds)
			} else if duration > start {
				end = duration
			}
		}
		if end <= start {
			continue
		}
		result = append(result, youTubeChapter{
			Title: chapter.Title,
			Start: start,
			End:   end,
		})
	}
	return result
}

func parseYouTubeURL(raw string) (videoID, canonical string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "www.youtube.com", "youtube.com", "m.youtube.com":
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		switch {
		case strings.Trim(u.Path, "/") == "watch":
			videoID = strings.TrimSpace(u.Query().Get("v"))
		case len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed"):
			videoID = strings.TrimSpace(parts[1])
		}
	case "youtu.be":
		videoID = strings.TrimSpace(strings.Trim(u.Path, "/"))
	}
	if videoID == "" {
		return "", "", false
	}
	return videoID, "https://www.youtube.com/watch?v=" + videoID, true
}

func secondsToDuration(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func formatDurationClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
