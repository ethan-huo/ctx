package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cache"
	"github.com/ethan-huo/ctx/cfrender"
	"github.com/ethan-huo/ctx/config"
)

// autoSplitThreshold: pages with ≤ this many screens are automatically split
// into per-screen images so the agent sees each at full resolution.
// LLM vision models down-sample overly tall images, losing detail.
const autoSplitThreshold = 3

type ScreenshotCmd struct {
	cfrender.DataFlag
	URL      string `arg:"" help:"URL to screenshot" optional:""`
	Output   string `short:"o" help:"Output file path (default: auto-generated)"`
	FullPage bool   `help:"Capture full page" default:"false"`
	Selector string `help:"Screenshot specific CSS selector element"`
	Scroll   int    `help:"Scroll Y offset in pixels (captures viewport-sized region at this offset)"`
	NoCache  bool   `help:"Bypass cache, always fetch fresh"`
}

func (c *ScreenshotCmd) Run(_ *api.Client) error {
	if c.Scroll > 0 && c.FullPage {
		return fmt.Errorf("--scroll and --full-page are mutually exclusive")
	}

	dataBody, err := c.ParseBody()
	if err != nil {
		return err
	}

	overrides := make(map[string]any)
	if c.URL != "" {
		overrides["url"] = c.URL
	}
	if c.Selector != "" {
		overrides["selector"] = c.Selector
	}

	body, err := config.BuildRequestBody("screenshot", c.URL, dataBody, overrides)
	if err != nil {
		return err
	}

	if c.URL == "" && dataBody == nil {
		return fmt.Errorf("URL is required (as argument or in -d body)")
	}

	url := effectiveURL(c.URL, body)

	// Selector or explicit clip in -d: direct mode (no full-page logic).
	if c.Selector != "" || hasExplicitClip(body) {
		return c.directScreenshot(body, url)
	}

	return c.smartScreenshot(body, url)
}

// ---------------------------------------------------------------------------
// Screenshot plan — pure decision logic, no IO
// ---------------------------------------------------------------------------

// screenshotPlan describes what to output from a full-page image.
type screenshotPlan struct {
	// screens lists the Y offsets to crop. Empty means use the full image as-is.
	screens []int
	// useFull outputs the full-page image without cropping (single-screen or --full-page).
	useFull bool
	// meta is the metadata line for the agent. Empty when not needed.
	meta string
}

// planScreenshot decides how to present a screenshot based on page dimensions and flags.
// Pure function: no IO, no side effects — all decisions are testable.
func planScreenshot(pageH, vpH, scroll int, fullPage bool) screenshotPlan {
	numScreens := (pageH + vpH - 1) / vpH
	if numScreens < 1 {
		numScreens = 1
	}

	switch {
	case fullPage:
		p := screenshotPlan{useFull: true}
		if numScreens > 1 {
			p.meta = fmt.Sprintf("full_page page=%d viewport=%d screens=%d", pageH, vpH, numScreens)
		}
		return p

	case scroll > 0:
		cur := scroll/vpH + 1
		p := screenshotPlan{screens: []int{scroll}}
		p.meta = fmt.Sprintf("page=%d viewport=%d screen=%d/%d", pageH, vpH, cur, numScreens)
		if cur < numScreens {
			p.meta += fmt.Sprintf(" (--scroll %d for next)", scroll+vpH)
		}
		return p

	case numScreens == 1:
		return screenshotPlan{useFull: true}

	case numScreens <= autoSplitThreshold:
		// Short multi-screen page: split into individual screen images.
		// LLM vision models down-sample tall images, so separate images
		// preserve detail better than one long strip.
		p := screenshotPlan{screens: make([]int, numScreens)}
		for i := range numScreens {
			p.screens[i] = i * vpH
		}
		p.meta = fmt.Sprintf("page=%d viewport=%d screens=%d", pageH, vpH, numScreens)
		return p

	default:
		// Long page: first screen only, with navigation hints.
		p := screenshotPlan{screens: []int{0}}
		p.meta = fmt.Sprintf("page=%d viewport=%d screen=1/%d (--scroll %d for next)", pageH, vpH, numScreens, vpH)
		return p
	}
}

// ---------------------------------------------------------------------------
// Screenshot execution
// ---------------------------------------------------------------------------

// directScreenshot: simple API call, no dimension awareness.
// Used for --selector and explicit screenshotOptions.clip in -d.
func (c *ScreenshotCmd) directScreenshot(body []byte, url string) error {
	cacheKey := cache.Key("screenshot", string(body))

	if !c.NoCache {
		if data, _, ok := cache.Lookup(cacheKey, ".png"); ok {
			return c.emitFile(cacheKey, data)
		}
	}

	client, err := cfrender.New()
	if err != nil {
		return err
	}

	data, err := client.Screenshot(context.Background(), c.URL, body)
	if err != nil {
		return fmt.Errorf("screenshot of %s failed: %w", url, err)
	}

	_ = cache.Store(cacheKey, data, ".png", cache.Meta{
		URL: url, Source: "cloudflare", ContentType: "image/png",
	})
	return c.emitFile(cacheKey, data)
}

// smartScreenshot captures a full-page image internally, plans the output
// strategy, then crops and emits the results with page dimension metadata.
// Subsequent --scroll calls reuse the cached full-page image (zero API cost).
func (c *ScreenshotCmd) smartScreenshot(body []byte, url string) error {
	vpH := viewportHeight(body)
	fullKey := stableCacheKey(body)

	// Try cached full-page image first.
	var fullData []byte
	if !c.NoCache {
		if data, _, ok := cache.Lookup(fullKey, ".png"); ok {
			fullData = data
		}
	}

	if fullData == nil {
		client, err := cfrender.New()
		if err != nil {
			return err
		}
		fullBody := withFullPage(body)
		fullData, err = client.Screenshot(context.Background(), c.URL, fullBody)
		if err != nil {
			return fmt.Errorf("screenshot of %s failed: %w", url, err)
		}
		_ = cache.Store(fullKey, fullData, ".png", cache.Meta{
			URL: url, Source: "cloudflare", ContentType: "image/png",
		})
	}

	pageW, pageH, err := pngSize(fullData)
	if err != nil {
		// Can't read dimensions — fall back to outputting raw data.
		key := cache.Key("screenshot", string(body))
		return c.emitFile(key, fullData)
	}

	plan := planScreenshot(pageH, vpH, c.Scroll, c.FullPage)

	return c.executePlan(plan, fullData, body, url, pageW, vpH)
}

// executePlan crops, caches, and outputs images according to the plan.
func (c *ScreenshotCmd) executePlan(plan screenshotPlan, fullData, body []byte, url string, pageW, vpH int) error {
	meta := cache.Meta{URL: url, Source: "cloudflare", ContentType: "image/png"}

	if plan.useFull {
		key := cache.Key("screenshot", string(body), fmt.Sprintf("s=%d,f=true", c.Scroll))
		_ = cache.Store(key, fullData, ".png", meta)
		outPath := c.outputPath(key)
		if err := os.WriteFile(outPath, fullData, 0o644); err != nil {
			return err
		}
		fmt.Println(outPath)
	} else {
		for i, scrollY := range plan.screens {
			cropped, err := cropPNG(fullData, 0, scrollY, pageW, vpH)
			if err != nil {
				return fmt.Errorf("crop at y=%d failed: %w", scrollY, err)
			}

			key := cache.Key("screenshot", string(body), fmt.Sprintf("s=%d", scrollY))
			_ = cache.Store(key, cropped, ".png", meta)

			// Use -o only for single-screen output.
			var outPath string
			if len(plan.screens) == 1 {
				outPath = c.outputPath(key)
			} else {
				outPath = cache.Path(key, ".png")
			}

			if err := os.WriteFile(outPath, cropped, 0o644); err != nil {
				return err
			}

			// Label each screen so the agent knows the order.
			if len(plan.screens) > 1 {
				fmt.Printf("%s [screen %d/%d]\n", outPath, i+1, len(plan.screens))
			} else {
				fmt.Println(outPath)
			}
		}
	}

	if plan.meta != "" {
		fmt.Println(plan.meta)
	}
	return nil
}

func (c *ScreenshotCmd) emitFile(cacheKey string, data []byte) error {
	outPath := c.outputPath(cacheKey)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}

func (c *ScreenshotCmd) outputPath(cacheKey string) string {
	if c.Output != "" {
		return c.Output
	}
	return cache.Path(cacheKey, ".png")
}

// ---------------------------------------------------------------------------
// Body helpers
// ---------------------------------------------------------------------------

// viewportHeight reads viewport.height from the merged body, defaulting to
// Puppeteer's 600px when unset.
func viewportHeight(body []byte) int {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return 600
	}
	if vp, ok := m["viewport"].(map[string]any); ok {
		if h, ok := vp["height"].(float64); ok && h > 0 {
			return int(h)
		}
	}
	return 600
}

// withFullPage returns a copy of body with screenshotOptions.fullPage = true
// and any clip removed.
func withFullPage(body []byte) []byte {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	ssOpts, _ := m["screenshotOptions"].(map[string]any)
	if ssOpts == nil {
		ssOpts = make(map[string]any)
	}
	ssOpts["fullPage"] = true
	delete(ssOpts, "clip")
	m["screenshotOptions"] = ssOpts
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// stableCacheKey returns a cache key for the full-page image that is independent
// of screenshotOptions (fullPage, clip, etc.) so different scroll offsets share
// the same cached source image.
func stableCacheKey(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return cache.Key("screenshot-full", string(body))
	}
	delete(m, "screenshotOptions")
	stable, err := json.Marshal(m)
	if err != nil {
		return cache.Key("screenshot-full", string(body))
	}
	return cache.Key("screenshot-full", string(stable))
}

// hasExplicitClip returns true if the body contains screenshotOptions.clip,
// indicating the user wants a specific crop region via -d.
func hasExplicitClip(body []byte) bool {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return false
	}
	ssOpts, ok := m["screenshotOptions"].(map[string]any)
	if !ok {
		return false
	}
	_, has := ssOpts["clip"]
	return has
}

// ---------------------------------------------------------------------------
// PNG utilities
// ---------------------------------------------------------------------------

// pngSize reads dimensions from a PNG without decoding pixel data.
func pngSize(data []byte) (width, height int, err error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// cropPNG extracts a rectangular region from a PNG image.
func cropPNG(data []byte, x, y, w, h int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	rect := image.Rect(x, y, x+w, y+h).Intersect(img.Bounds())
	if rect.Empty() {
		return nil, fmt.Errorf("crop region (%d,%d %dx%d) outside image bounds %v", x, y, w, h, img.Bounds())
	}

	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
