package cmd

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// BDD: planScreenshot — pure decision logic
// ---------------------------------------------------------------------------

func TestPlanScreenshot(t *testing.T) {

	t.Run("single screen page outputs full image with no metadata", func(t *testing.T) {
		plan := planScreenshot(600, 600, 0, false)

		if !plan.useFull {
			t.Error("expected useFull=true for single screen")
		}
		if len(plan.screens) != 0 {
			t.Errorf("expected no screen slices, got %d", len(plan.screens))
		}
		if plan.meta != "" {
			t.Errorf("expected no metadata for single screen, got %q", plan.meta)
		}
	})

	t.Run("two screen page splits into two separate images", func(t *testing.T) {
		plan := planScreenshot(1200, 600, 0, false)

		if plan.useFull {
			t.Error("expected useFull=false for multi-screen split")
		}
		if len(plan.screens) != 2 {
			t.Fatalf("expected 2 screens, got %d", len(plan.screens))
		}
		if plan.screens[0] != 0 {
			t.Errorf("screen 1 offset = %d, want 0", plan.screens[0])
		}
		if plan.screens[1] != 600 {
			t.Errorf("screen 2 offset = %d, want 600", plan.screens[1])
		}
		if !strings.Contains(plan.meta, "screens=2") {
			t.Errorf("metadata should contain screens=2, got %q", plan.meta)
		}
	})

	t.Run("three screen page splits into three separate images", func(t *testing.T) {
		plan := planScreenshot(2700, 900, 0, false)

		if len(plan.screens) != 3 {
			t.Fatalf("expected 3 screens, got %d", len(plan.screens))
		}
		if plan.screens[2] != 1800 {
			t.Errorf("screen 3 offset = %d, want 1800", plan.screens[2])
		}
		if !strings.Contains(plan.meta, "screens=3") {
			t.Errorf("metadata should contain screens=3, got %q", plan.meta)
		}
	})

	t.Run("partial last screen still counts as a screen", func(t *testing.T) {
		// 1400px page / 600px viewport = 2.33 → 3 screens
		plan := planScreenshot(1400, 600, 0, false)

		if len(plan.screens) != 3 {
			t.Fatalf("expected 3 screens for 1400/600, got %d", len(plan.screens))
		}
		if plan.screens[2] != 1200 {
			t.Errorf("screen 3 offset = %d, want 1200", plan.screens[2])
		}
	})

	t.Run("four screen page crops first screen with navigation hint", func(t *testing.T) {
		plan := planScreenshot(3600, 900, 0, false)

		if plan.useFull {
			t.Error("should not use full image for long pages")
		}
		if len(plan.screens) != 1 {
			t.Fatalf("expected 1 screen for long page, got %d", len(plan.screens))
		}
		if plan.screens[0] != 0 {
			t.Errorf("screen offset = %d, want 0", plan.screens[0])
		}
		if !strings.Contains(plan.meta, "screen=1/4") {
			t.Errorf("metadata should show screen=1/4, got %q", plan.meta)
		}
		if !strings.Contains(plan.meta, "--scroll 900") {
			t.Errorf("metadata should hint --scroll 900, got %q", plan.meta)
		}
	})

	t.Run("scroll to middle of long page crops at offset with next hint", func(t *testing.T) {
		plan := planScreenshot(5400, 900, 1800, false)

		if len(plan.screens) != 1 || plan.screens[0] != 1800 {
			t.Fatalf("expected single screen at offset 1800, got %v", plan.screens)
		}
		if !strings.Contains(plan.meta, "screen=3/6") {
			t.Errorf("expected screen=3/6, got %q", plan.meta)
		}
		if !strings.Contains(plan.meta, "--scroll 2700") {
			t.Errorf("expected next scroll hint, got %q", plan.meta)
		}
	})

	t.Run("scroll to last screen has no next hint", func(t *testing.T) {
		plan := planScreenshot(1800, 900, 900, false)

		if !strings.Contains(plan.meta, "screen=2/2") {
			t.Errorf("expected screen=2/2, got %q", plan.meta)
		}
		if strings.Contains(plan.meta, "--scroll") {
			t.Errorf("last screen should not have next hint, got %q", plan.meta)
		}
	})

	t.Run("full-page flag outputs complete image with screen count", func(t *testing.T) {
		plan := planScreenshot(5400, 900, 0, true)

		if !plan.useFull {
			t.Error("--full-page should set useFull=true")
		}
		if len(plan.screens) != 0 {
			t.Errorf("--full-page should not produce screen slices, got %d", len(plan.screens))
		}
		if !strings.Contains(plan.meta, "full_page") {
			t.Errorf("metadata should indicate full_page, got %q", plan.meta)
		}
		if !strings.Contains(plan.meta, "screens=6") {
			t.Errorf("metadata should show screens=6, got %q", plan.meta)
		}
	})

	t.Run("full-page of single screen page has no metadata", func(t *testing.T) {
		plan := planScreenshot(600, 600, 0, true)

		if !plan.useFull {
			t.Error("expected useFull=true")
		}
		if plan.meta != "" {
			t.Errorf("single-screen full-page should have no metadata, got %q", plan.meta)
		}
	})

	t.Run("metadata always includes page height and viewport height", func(t *testing.T) {
		cases := []struct {
			name   string
			pageH  int
			vpH    int
			scroll int
			full   bool
		}{
			{"split-2", 1800, 900, 0, false},
			{"long-page", 5400, 900, 0, false},
			{"scroll", 5400, 900, 900, false},
			{"full-page", 5400, 900, 0, true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				plan := planScreenshot(tc.pageH, tc.vpH, tc.scroll, tc.full)
				if !strings.Contains(plan.meta, "page=") {
					t.Errorf("missing page= in metadata: %q", plan.meta)
				}
				if !strings.Contains(plan.meta, "viewport=") {
					t.Errorf("missing viewport= in metadata: %q", plan.meta)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// BDD: PNG crop integration — verify actual image splitting
// ---------------------------------------------------------------------------

// makePNG creates a test PNG with a vertical color gradient so each "screen"
// region is visually distinct and verifiable after cropping.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 0, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestCropPNG_SplitThreeScreens(t *testing.T) {
	t.Run("splitting a 3-screen image produces 3 correctly sized PNGs", func(t *testing.T) {
		fullData := makePNG(800, 1800)
		vpH := 600

		for i := range 3 {
			y := i * vpH
			cropped, err := cropPNG(fullData, 0, y, 800, vpH)
			if err != nil {
				t.Fatalf("crop screen %d failed: %v", i+1, err)
			}
			w, h, err := pngSize(cropped)
			if err != nil {
				t.Fatalf("pngSize screen %d failed: %v", i+1, err)
			}
			if w != 800 || h != vpH {
				t.Errorf("screen %d: %dx%d, want %dx%d", i+1, w, h, 800, vpH)
			}
		}
	})

	t.Run("last screen of uneven page is shorter", func(t *testing.T) {
		fullData := makePNG(800, 1400)

		// Screen 3: starts at y=1200, only 200px remain
		cropped, err := cropPNG(fullData, 0, 1200, 800, 600)
		if err != nil {
			t.Fatal(err)
		}
		_, h, _ := pngSize(cropped)
		if h != 200 {
			t.Errorf("last screen height = %d, want 200 (clamped to image bounds)", h)
		}
	})

	t.Run("crop beyond image bounds returns error", func(t *testing.T) {
		fullData := makePNG(800, 600)
		_, err := cropPNG(fullData, 0, 700, 800, 600)
		if err == nil {
			t.Error("expected error for out-of-bounds crop")
		}
	})
}

func TestPngSize(t *testing.T) {
	data := makePNG(1440, 2700)
	w, h, err := pngSize(data)
	if err != nil {
		t.Fatal(err)
	}
	if w != 1440 || h != 2700 {
		t.Errorf("pngSize = %dx%d, want 1440x2700", w, h)
	}
}

// ---------------------------------------------------------------------------
// BDD: body helpers
// ---------------------------------------------------------------------------

func TestViewportHeight(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"defaults to 600 when no viewport", `{"url":"x"}`, 600},
		{"reads explicit viewport height", `{"url":"x","viewport":{"width":1440,"height":900}}`, 900},
		{"defaults when height missing", `{"url":"x","viewport":{"width":1440}}`, 600},
		{"defaults on invalid json", `not json`, 600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := viewportHeight([]byte(tt.body))
			if got != tt.want {
				t.Errorf("viewportHeight = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWithFullPage(t *testing.T) {
	t.Run("sets fullPage and removes clip while preserving other options", func(t *testing.T) {
		body := []byte(`{"url":"x","screenshotOptions":{"quality":80,"clip":{"x":0,"y":0}}}`)
		got := withFullPage(body)

		var m map[string]any
		json.Unmarshal(got, &m)
		ss := m["screenshotOptions"].(map[string]any)

		if ss["fullPage"] != true {
			t.Error("fullPage not set")
		}
		if _, ok := ss["clip"]; ok {
			t.Error("clip should be removed")
		}
		if ss["quality"] != float64(80) {
			t.Error("quality should be preserved")
		}
	})
}

func TestStableCacheKey(t *testing.T) {
	t.Run("same URL with different screenshotOptions produces same key", func(t *testing.T) {
		body1 := []byte(`{"url":"x","viewport":{"height":900}}`)
		body2 := []byte(`{"url":"x","viewport":{"height":900},"screenshotOptions":{"fullPage":true}}`)

		if stableCacheKey(body1) != stableCacheKey(body2) {
			t.Error("stable keys should match regardless of screenshotOptions")
		}
	})

	t.Run("different URLs produce different keys", func(t *testing.T) {
		body1 := []byte(`{"url":"x"}`)
		body2 := []byte(`{"url":"y"}`)

		if stableCacheKey(body1) == stableCacheKey(body2) {
			t.Error("different URLs should produce different keys")
		}
	})
}

func TestHasExplicitClip(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"no screenshotOptions", `{"url":"x"}`, false},
		{"fullPage only", `{"url":"x","screenshotOptions":{"fullPage":true}}`, false},
		{"has clip", `{"url":"x","screenshotOptions":{"clip":{"x":0,"y":100}}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExplicitClip([]byte(tt.body))
			if got != tt.want {
				t.Errorf("hasExplicitClip = %v, want %v", got, tt.want)
			}
		})
	}
}
