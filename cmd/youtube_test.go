package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestParseYouTubeURL(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantVideoID   string
		wantCanonical string
		wantOK        bool
	}{
		{
			name:          "watch url",
			input:         "https://www.youtube.com/watch?v=rIwgZWzUKm8&t=15s",
			wantVideoID:   "rIwgZWzUKm8",
			wantCanonical: "https://www.youtube.com/watch?v=rIwgZWzUKm8",
			wantOK:        true,
		},
		{
			name:          "short url",
			input:         "https://youtu.be/rIwgZWzUKm8?si=abc",
			wantVideoID:   "rIwgZWzUKm8",
			wantCanonical: "https://www.youtube.com/watch?v=rIwgZWzUKm8",
			wantOK:        true,
		},
		{
			name:          "shorts url",
			input:         "https://www.youtube.com/shorts/rIwgZWzUKm8",
			wantVideoID:   "rIwgZWzUKm8",
			wantCanonical: "https://www.youtube.com/watch?v=rIwgZWzUKm8",
			wantOK:        true,
		},
		{
			name:          "embed url",
			input:         "https://www.youtube.com/embed/rIwgZWzUKm8?start=30",
			wantVideoID:   "rIwgZWzUKm8",
			wantCanonical: "https://www.youtube.com/watch?v=rIwgZWzUKm8",
			wantOK:        true,
		},
		{
			name:   "non youtube url",
			input:  "https://example.com/watch?v=rIwgZWzUKm8",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotCanonical, ok := parseYouTubeURL(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if gotID != tt.wantVideoID {
				t.Fatalf("videoID = %q, want %q", gotID, tt.wantVideoID)
			}
			if gotCanonical != tt.wantCanonical {
				t.Fatalf("canonical = %q, want %q", gotCanonical, tt.wantCanonical)
			}
		})
	}
}

func TestParseYouTubeMetadata_PrefersManualSubtitles(t *testing.T) {
	raw := []byte(`{
		"id":"abc123",
		"title":"Transcript Test",
		"channel":"Ctx",
		"duration":3670,
		"chapters":[
			{"title":"<Untitled Chapter 1>","start_time":0,"end_time":70},
			{"title":"Core section","start_time":70,"end_time":120}
		],
		"subtitles":{
			"zh":[{"ext":"json3","url":"https://subs.test/zh.json3","name":"Chinese"}],
			"en":[{"ext":"json3","url":"https://subs.test/en.json3","name":"English"}]
		},
		"automatic_captions":{
			"ja":[{"ext":"json3","url":"https://subs.test/ja-auto.json3","name":"Japanese auto"}]
		}
	}`)

	meta, err := parseYouTubeMetadata(raw, "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("parseYouTubeMetadata returned error: %v", err)
	}
	if meta.CaptionKind != "subtitles" {
		t.Fatalf("CaptionKind = %q, want subtitles", meta.CaptionKind)
	}
	if meta.CaptionLang != "en" {
		t.Fatalf("CaptionLang = %q, want en", meta.CaptionLang)
	}
	if len(meta.CaptionTracks) != 2 {
		t.Fatalf("CaptionTracks = %d, want 2", len(meta.CaptionTracks))
	}
	if len(meta.Chapters) != 2 {
		t.Fatalf("chapters = %d, want 2", len(meta.Chapters))
	}
}

func TestParseYouTubeCues_StripsMarkup(t *testing.T) {
	cues := parseYouTubeCues([]struct {
		StartMs    int `json:"tStartMs"`
		DurationMs int `json:"dDurationMs"`
		Segments   []struct {
			Text string `json:"utf8"`
		} `json:"segs"`
	}{
		{
			StartMs:    1200,
			DurationMs: 800,
			Segments: []struct {
				Text string `json:"utf8"`
			}{
				{Text: "<b>Hello</b>"},
				{Text: " world"},
			},
		},
	})

	if len(cues) != 1 {
		t.Fatalf("cues = %d, want 1", len(cues))
	}
	if cues[0].Text != "Hello world" {
		t.Fatalf("cue text = %q, want %q", cues[0].Text, "Hello world")
	}
}

func TestLooksLikeTranslatedYouTubeTrack(t *testing.T) {
	if !looksLikeTranslatedYouTubeTrack([]youTubeCue{
		{Text: "This subtitle was translated by AI. We cannot guarantee its accuracy."},
		{Text: "Hello, everyone"},
	}) {
		t.Fatal("expected AI translation disclaimer to be detected")
	}
	if looksLikeTranslatedYouTubeTrack([]youTubeCue{
		{Text: "哈喽 大家好"},
		{Text: "我是小珺"},
	}) {
		t.Fatal("clean native subtitles should not be treated as translated")
	}
}

func TestRenderYouTubeTranscript_UsesChapterSections(t *testing.T) {
	meta := youTubeMetadata{
		Title:        "Video",
		Channel:      "Ctx",
		Duration:     2 * time.Hour,
		CaptionKind:  "subtitles",
		CaptionLang:  "en",
		CanonicalURL: "https://www.youtube.com/watch?v=abc123",
		Chapters: []youTubeChapter{
			{Start: 0, End: 10 * time.Minute, Title: "<Untitled Chapter 1>"},
			{Start: 10 * time.Minute, End: 20 * time.Minute, Title: "Deep Dive"},
		},
	}
	cues := []youTubeCue{
		{Start: 5 * time.Second, End: 8 * time.Second, Text: "Opening line"},
		{Start: 12 * time.Minute, End: 12*time.Minute + 3*time.Second, Text: "Important point"},
	}

	doc := renderYouTubeTranscript(meta, cues)
	if !strings.Contains(doc, "Title: Video") {
		t.Fatalf("missing plain title header, got:\n%s", doc)
	}
	if !strings.Contains(doc, "# 00:00:00-00:10:00") {
		t.Fatalf("missing first time section, got:\n%s", doc)
	}
	if !strings.Contains(doc, "# 00:10:00-00:20:00 — Deep Dive") {
		t.Fatalf("missing titled section, got:\n%s", doc)
	}
	if !strings.Contains(doc, "[00:12:00] Important point") {
		t.Fatalf("missing cue text, got:\n%s", doc)
	}
}

func TestRenderYouTubeTranscript_FallsBackToWindows(t *testing.T) {
	meta := youTubeMetadata{
		Title:       "Windowed",
		Duration:    31 * time.Minute,
		CaptionKind: "automatic captions",
		CaptionLang: "en",
	}
	cues := []youTubeCue{
		{Start: 5 * time.Second, End: 8 * time.Second, Text: "Intro"},
		{Start: 16 * time.Minute, End: 16*time.Minute + 3*time.Second, Text: "Middle"},
		{Start: 30 * time.Minute, End: 30*time.Minute + 3*time.Second, Text: "End"},
	}

	doc := renderYouTubeTranscript(meta, cues)
	if !strings.Contains(doc, "# 00:00:00-00:15:00") {
		t.Fatalf("missing first window section, got:\n%s", doc)
	}
	if !strings.Contains(doc, "# 00:15:00-00:30:00") {
		t.Fatalf("missing second window section, got:\n%s", doc)
	}
	if !strings.Contains(doc, "# 00:30:00-00:31:00") {
		t.Fatalf("missing final window section, got:\n%s", doc)
	}
}

func TestReadFetch_YouTubeUsesTranscriptPath(t *testing.T) {
	oldMetaLoader := youtubeMetadataLoader
	oldSubtitleLoader := youtubeSubtitleLoader
	t.Cleanup(func() {
		youtubeMetadataLoader = oldMetaLoader
		youtubeSubtitleLoader = oldSubtitleLoader
	})

	youtubeMetadataLoader = func(rawURL string) (youTubeMetadata, error) {
		return youTubeMetadata{
			VideoID:      "abc123",
			CanonicalURL: "https://www.youtube.com/watch?v=abc123",
			Title:        "Test Video",
			Channel:      "Ctx",
			Duration:     20 * time.Minute,
			CaptionKind:  "subtitles",
			CaptionLang:  "en",
			CaptionURL:   "https://subs.test/en.json3",
			CaptionTracks: []youTubeTrackSelection{
				{
					Kind: "subtitles",
					Lang: "en",
					Name: "English",
					URL:  "https://subs.test/en.json3",
				},
			},
		}, nil
	}
	youtubeSubtitleLoader = func(subtitleURL string) ([]youTubeCue, error) {
		if subtitleURL != "https://subs.test/en.json3" {
			t.Fatalf("subtitleURL = %q, want %q", subtitleURL, "https://subs.test/en.json3")
		}
		return []youTubeCue{
			{Start: 2 * time.Second, End: 4 * time.Second, Text: "Hello"},
		}, nil
	}

	content, source, err := (&ReadCmd{}).fetch("https://www.youtube.com/watch?v=abc123&t=5s", nil)
	if err != nil {
		t.Fatalf("fetch returned error: %v", err)
	}
	if source != "youtube" {
		t.Fatalf("source = %q, want youtube", source)
	}
	if !strings.Contains(content, "Title: Test Video") || !strings.Contains(content, "[00:00:02] Hello") {
		t.Fatalf("unexpected content:\n%s", content)
	}
}

func TestFetchYouTubeTranscript_PrefersNativeTrackOverTranslatedDisclaimer(t *testing.T) {
	oldMetaLoader := youtubeMetadataLoader
	oldSubtitleLoader := youtubeSubtitleLoader
	t.Cleanup(func() {
		youtubeMetadataLoader = oldMetaLoader
		youtubeSubtitleLoader = oldSubtitleLoader
	})

	youtubeMetadataLoader = func(rawURL string) (youTubeMetadata, error) {
		return youTubeMetadata{
			VideoID:      "abc123",
			CanonicalURL: "https://www.youtube.com/watch?v=abc123",
			Title:        "Test Video",
			Channel:      "Ctx",
			Duration:     20 * time.Minute,
			CaptionTracks: []youTubeTrackSelection{
				{Kind: "subtitles", Lang: "en", Name: "English", URL: "https://subs.test/en.json3"},
				{Kind: "subtitles", Lang: "zh", Name: "Chinese", URL: "https://subs.test/zh.json3"},
			},
		}, nil
	}
	youtubeSubtitleLoader = func(subtitleURL string) ([]youTubeCue, error) {
		switch subtitleURL {
		case "https://subs.test/en.json3":
			return []youTubeCue{
				{Start: 0, End: time.Second, Text: "This subtitle was translated by AI. We cannot guarantee its accuracy."},
				{Start: time.Second, End: 2 * time.Second, Text: "Hello"},
			}, nil
		case "https://subs.test/zh.json3":
			return []youTubeCue{
				{Start: 0, End: time.Second, Text: "哈喽 大家好"},
				{Start: time.Second, End: 2 * time.Second, Text: "我是小珺"},
			}, nil
		default:
			t.Fatalf("unexpected subtitleURL: %s", subtitleURL)
			return nil, nil
		}
	}

	content, err := fetchYouTubeTranscript("https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("fetchYouTubeTranscript returned error: %v", err)
	}
	if !strings.Contains(content, "- language: zh") {
		t.Fatalf("expected zh track to win, got:\n%s", content)
	}
	if !strings.Contains(content, "[00:00:00] 哈喽 大家好") {
		t.Fatalf("expected native subtitle content, got:\n%s", content)
	}
	if strings.Contains(content, "translated by AI") {
		t.Fatalf("translated disclaimer should have been skipped, got:\n%s", content)
	}
}
